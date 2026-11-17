# 代码冗余与无用设计梳理

审查日期：2026-11-04。基于当前工作区，包含尚未提交的11月3日登录与启动参数调整，不只看GitHub的v1.0.4。

后续执行状态：优先项已完成第一轮清理及试点部署，见[CLEANUP-2026-11-04.md](CLEANUP-2026-11-04.md)。下文保留清理前的审查证据和当时检查结果。

主要冗余集中在旧功能退出后的残留、重复部署路径，以及前端轮询范围过大。建议分批清理；核心采集、受控重启、持久化恢复有明确职责，保留这些职责。

本次仅新增审查报告，没有删除或重构业务代码，也没有更新服务器、GitHub和离线包。

## 审查范围与证据

- 检查monitor、notification_receiver、v1_api_prototype三个Go模块，React源码，以及部署、打包、测试入口。
- Go使用staticcheck的U1000检查，并用golang.org/x/tools/cmd/deadcode v0.24.0分别分析程序入口和包含测试的入口；结合全库调用点搜索人工确认。
- 前端执行常规TypeScript类型检查，以及额外的noUnusedLocals/noUnusedParameters检查；人工检查导出组件、状态和属性。
- CSS按源码引用筛候选，再对照已退出的组件；不把Ant Design动态注入的类名直接判为无用。
- 不把node_modules、已生成的静态JS/CSS、历史离线包当作手写源码重复统计。

调用图分析对反射、接口和序列化较保守，TypeScript未使用检查也不报告所有未引用的导出，不能只凭单一工具自动删除。以下明确区分可清理项与仍需兼容的部分。

## 一、确定存在的残留，适合先清理

| 编号 | 位置 | 证据与建议 |
| --- | --- | --- |
| D01 | internal/access/registry.go:126、146、150 | Registry.Apply已无程序或测试入口可达；Issue在146行拒绝所有非空RequestID，150行又判断相同条件并执行旧审批流程，该分支不可能运行。删除申请方法及不可达签发分支；历史记录字段和表的处理见兼容项。 |
| D02 | internal/docker/collector.go:247；internal/model/operations.go:5、11；internal/center/resources.go:11 | WSS按需获取入口已经移除，但Collector.Startup、StartupRequest、StartupResult仍存在，无当前调用方；Store.NodeStartup仅剩测试调用。测试改用实际服务状态返回值后可删除这些遗留入口。必须保留实际采集使用的startup(raw,...)、Collector.Collect和startupViewLocked。 |
| D03 | frontend/src/App.tsx:1581 | PortsModal没有引用或渲染入口；当前端口配置由NodeSettingsModal负责。删除整个旧弹窗，不删除现用节点设置。 |
| D04 | frontend/src/MetricUI.tsx:70、94、123 | MetricRow、MetricPanel、Meter三个导出组件无调用方，是旧资源面板残留。删除它们及专属依赖，保留仍在使用的MetricIcon。 |
| D05 | frontend/src/Accounts.tsx、App.tsx、Operations.tsx、Resources.tsx | 开启未使用检查后共12项诊断，见下表。包括旧导入、无效属性与永远不更新的备用展开状态。 |
| D06 | internal/postgres/postgres.go:325；internal/access/registry.go:72 | randomID被staticcheck和调用图共同确认无用；ErrDenied全库仅有声明、没有使用。删除函数/变量后清理仅由它们使用的import，不能机械删除同文件其他仍在用的加密或随机数依赖。 |
| D07 | frontend/src/App.tsx:128、236；frontend/src/api.ts:10 | 登录mode固定为ACCOUNT，没有修改入口，仍保留mode参数、Basic凭证构造和账号/Basic分支。前端登录可以直接按账号登录实现，去掉固定mode和被覆盖的Basic计算。后端Agent和诊断Basic鉴权是另外的链路，不能一起删。 |
| D08 | cmd/agent/main.go:64；deploy/docker-compose.agent.yml:13；deploy/clean-install-check.py:92 | Agent已固定RawStartup:true，AVOPS_RAW_STARTUP不再读取；传入的EnvAllow/ArgAllow在raw路径也不参与参数处理。部署材料仍设置这些开关，文档还描述开关行为，容易误导。清理无效环境变量和说明；如决定移除整个脱敏策略分支，须另外核对库调用与测试，不能连带删掉仍被使用的通用安全处理。 |

### TypeScript的12项未使用诊断

| 文件 | 未使用项 |
| --- | --- |
| Accounts.tsx | Pagination、Checkbox导入 |
| App.tsx | Radio、Space导入；PortsModal；NodeSettingsModal的revision参数 |
| Operations.tsx | steps.map的index参数 |
| Resources.tsx | Empty导入；rate函数；OverviewItem的ratio参数；onDetailsOpenChange参数；setInternalDetailsOpen状态更新器 |

Resources的展开由App的detailsOpen控制，内部备用状态只初始化为false，没有更新入口，传入的onDetailsOpenChange也未调用。建议统一成受控组件，连同父页面无效回调和ratio传参一起清理，避免只删局部声明后继续留下空壳接口。

### CSS残留候选

frontend/src/style.css共1195行。静态搜索得到20个在当前TS/TSX中没有完整字面引用的项目类名候选，另有已退出MetricPanel组件对应的样式族。候选不是直接删除结论：应按组件族核对动态类名、响应式布局及实际DOM后再清理。

已明确对应旧设计的部分包括：

- node-scope-card、node-scope-grid：旧Token节点范围选择。
- resource-tile、metric-panel-grid及metric-panel相关规则：旧资源卡片与面板。
- startup-image-row、startup-image、startup-command、startup-command-heading、startup-focused、startup-additional、is-expanded：启动参数改整表前的布局。
- filesystem-hero、device-list、device-heading、device-metrics：旧文件系统/设备布局。

优先核对style.css:413起的旧面板规则、859起的紧凑面板覆盖，以及926起的旧启动参数规则。不要整段删除其中仍有效的startup-toolbar、startup-unified、resource-trend-*等样式；相邻media规则也要逐条看。Ant Design的anticon等动态类名不能按“源码未出现”删除。

## 二、仍在运行，但有重复实现或多余工作

### R01：全局轮询范围过大，运行收益最值得优先处理

frontend/src/App.tsx:416的reload每次按权限并行查询台账、Agent、服务状态、容器发现、操作、通知状态。15秒定时器不区分当前页签；存在活动操作时，另一个2秒定时器仍调用同一个reload，重复查询全部模块，并且原15秒定时器仍在。

这不是死代码，但会让未打开的模块持续查询。11月3日起服务状态还附带启动参数，活动操作期间也会反复传输这些较大的数据；两个定时器可能产生重叠请求，请求代次只阻止旧结果覆盖，不能取消已经发出的查询。

建议：服务状态保持必要的轮询；容器发现、账号、通知配置等按页签进入及保存后更新；执行中的重启只加速操作结果查询；合并重复触发或增加在途请求控制。启动参数继续随现有状态数据返回，不重新增加独立刷新请求。

### R02：多份镜像构建定义

deploy/Dockerfile.center与Dockerfile.center.runtime内容完全一致；Dockerfile.agent与agent.runtime主要区别只是显式USER 0:0。deploy/build-images.py:14起又动态拼接四类镜像的Dockerfile。

建议只选一种权威定义：让打包器读取维护中的模板，或统一由打包器生成并退役旧文件。Dockerfile.canary仍被干净安装验收实际调用，不能跟着删。

### R03：封包流程重复

deploy/build-release.sh:35起和deploy/seal-release.sh:7起重复执行镜像校验、文件校验、压缩。seal-release.sh额外生成归档自身的sha256文件，而build-release.sh没有，行为已不完全一致。

建议build-release在完成编译、复制后调用统一封包入口；保留镜像内容校验、逐文件SHA256和最终归档SHA256，它们检查的对象不同，不属于重复保障。

### R04：同类格式化逻辑与大组件

Resources.tsx、ResourceTrends.tsx、StorageStatus.tsx都有容量格式化；部分时间处理也分散在App、Operations和Resources。可以共享底层单位换算，再由各页面决定精度、空值和文字，不能把不同时间语义强行合并。

App.tsx约1955行，包含服务状态、发现、台账及多个弹窗。它不是整文件无用，但将查询、状态和页面拆开，有助于消除轮询耦合、重复表单及无效属性。优先删掉确定无用部分，再做模块拆分。

### R05：固定角色仍保留“保存角色”的空壳

internal/identity/administration.go:180保留RoleUpdate/SaveRole，而SaveRole永远返回FIXED_ROLES。internal/center/identity_http.go仍为角色POST读取请求、反序列化、调用方法并保留成功分支。

当前角色固定，角色GET和用户分配角色仍要保留。写角色接口可以在路由层明确拒绝，收掉无用更新结构和成功分支；是否继续返回同一个错误码由接口兼容要求决定。

## 三、有历史兼容用途，不能直接按无用代码删除

| 项目 | 当前用途 | 收敛方式 |
| --- | --- | --- |
| 通知的单地址、三类Routes、新Endpoints三套模型 | notify/settings.go、targets.go、endpoints.go仍读旧配置，并将旧路由映射为新配置；旧待投递记录也有不同标识方式 | 做一次明确的持久化迁移与队列验证后，运行时统一Endpoints。不要直接删除旧读取代码而丢失地址、签名密钥或投递任务。 |
| Key申请历史字段和key_requests表 | 创建申请的方法已不可达，但record.Request、恢复映射、projections和数据库历史结构仍保留旧数据 | 先停用旧创建逻辑，再决定旧记录只读保留或迁移。删除新申请代码不等于直接DROP历史表。 |
| 文件存储与PostgreSQL双实现 | 正式中心要求PG；file-dev、fixture、部分隔离测试仍使用文件/内存后端。Agent执行账本也必须独立于中心DB | 将开发模式入口隔离得更明确；Agent本地账本不能改成依赖中心DB或作为重复存储删除。 |
| 导入、导出、操作压缩、旧节点范围清理 | dbmigrate仍调用；用于升级旧环境、恢复旧数据或处理已发布版本 | 保留为迁移工具，运行时减少分散兼容分支；经过版本支持期确认后再退役。CompactOperations已有完成标记，不能说它每次启动都全量改写操作。 |
| 二进制systemd部署与Docker部署 | 中心/Agent主路径已转Docker，旧install、rollback和service仍与早期手册相连 | 整理为Docker正式路径和legacy材料。既有交付记录中的cAdvisor及201接收端仍有systemd部署，不能把所有.service一刀切删掉。 |
| 后端Basic鉴权 | Agent WSS和运维诊断仍用，前端只用账号登录不代表整个系统不需要Basic | 可清理前端旧登录分支；后端先按Agent、诊断、账号、重启Token职责拆开。旧密钥对用于识别后返回TOKEN_REQUIRED的代码，应作为兼容错误处理单独决定。 |
| Mock、本地viewer、canary、acceptance工具 | 开发、演示、离线验收和受限访问有用途；这些不是普通生产API入口 | 分清交付和开发目录，按需打包，不按“生产没有调用”直接删整个工具。 |
| 静态JS/CSS与TS源码 | Go中心嵌入构建后的前端，离线运行需要静态文件 | 保留生成资源；只清理不再引用的旧构建产物，不能把两份形态当作重复源码删除。 |

补充：notification_receiver/main.go:167的store.accept只用于测试，实际入口使用acceptBody。可以将便利包装移入_test.go；它不是接收服务业务路径冗余，也不代表接收端存储逻辑可以删。v1_api_prototype未发现调用图意义上的无用函数。

## 四、测试和部署材料已经出现过期内容

1. monitor/tests/ui.cjs:17仍填写“接入ID/接入密钥”，后面查“容器名称”和旧分页、查看弹窗。当前页面使用账号密码、主机IP/运行态及新发现交互，静态确认脚本与当前页面不匹配。browser_test.go:18默认跳过该测试，普通go test通过不能证明此浏览器链路可用。本次未运行该可选浏览器测试。建议改为当前流程的可重复回归，保留筛选、选择和保存不会覆盖台账的测试目标。
2. deploy/docker-compose.center.yml:3和agent.yml:3仍写1.0.0，正式离线交付已到1.0.4；Agent Compose还绑定restart-demo-agent-rules.yaml。它们不应与新安装器并列充当未注明版本的正式入口。
3. README、OPERATIONS中仍有“默认脱敏/RAW开关”等早期描述，与当前Agent固定原值不一致。文档应以当前部署入口及配置表为准，历史记录单独标明。
4. v1.0.4离线包是已发布历史制品，不应覆盖或当作垃圾删除；11月3日的参数合并返回和登录调整仍是工作区/线上更新，需要在下一次正式发包时进入新版本。

## 五、建议的清理顺序

| 批次 | 工作 | 验证重点 |
| --- | --- | --- |
| 第一批：低风险清理 | 删除申请不可达分支、旧端口弹窗、无调用指标组件、未使用声明；退出按需参数残留，测试改到现用状态接口；按组件族清CSS；清理无效配置与前端固定登录模式 | 常规TS检查、启用noUnused检查、Go检查；页面验证参数/曲线/台账/Token；更新涉及的测试，不靠删测试掩盖问题 |
| 第二批：降低维护与运行开销 | 页签查询解耦、活动重启只轮询操作；统一Docker构建和封包入口；明确部署材料版本；更新浏览器回归 | 请求次数与页面切换、活动操作恢复、全服务离线干净安装 |
| 第三批：兼容迁移 | 通知模型统一、申请历史与旧持久化边界、systemd旧路径归档 | 用真实格式的旧配置/队列/操作记录恢复测试；不丢通知、不丢重启记录与身份归属 |

重启幂等、节点互斥、执行结果核验、双端白名单、Agent本地执行账本、通知签名/重试/去重、参数缓存和资源历史应继续保留。这些实现承担不同的故障恢复责任，清理应针对退出功能和重复实现。

主要收益是缩小维护面、避免旧入口误导，以及减少无关轮询。部分未引用前端代码可能已被构建器裁剪，不能按删除行数推断镜像体积会明显下降。

## 检查结果摘要

| 检查 | 结果 |
| --- | --- |
| TypeScript常规类型检查 | 通过 |
| TypeScript额外未使用检查 | 12项诊断，未自动修复 |
| monitor的U1000 | randomID无用 |
| monitor调用图，不含测试 | Registry.Apply、Store.NodeStartup、randomID不可达 |
| monitor调用图，包含测试 | Registry.Apply、randomID不可达；NodeStartup为测试保留 |
| notification_receiver的U1000 | 未报告无用声明 |
| notification_receiver调用图，不含测试 | store.accept只供测试辅助 |
| v1_api_prototype的U1000与调用图 | 未报告对应问题 |

本轮为静态和调用关系审查，没有重新进行现场故障、业务重启或全包安装验证。
