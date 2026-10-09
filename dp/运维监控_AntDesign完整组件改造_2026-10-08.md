# Ant Design 完整组件改造

日期：2026-10-08。使用 Ant Design 5.29，React 19；正式中心为 111.230.108.76，本机入口为 http://127.0.0.1:18086/。

## 页面落点

| 页面 | 实际使用组件 |
|---|---|
| 登录、注册、改密 | Card、Form、Input、Input.Password、Button、Alert、Modal |
| 产品栏与模块导航 | Layout、Typography、Tabs、Dropdown |
| 服务状态与资源 | Card、Statistic、Tag、Collapse、Tabs、Descriptions、Progress、Timeline |
| 容器发现、当前基准 | Form、Select、Table、行选择、Pagination、Card、Modal |
| 操作与审计 | Card、Tag、Modal、Form、Input、Select、Checkbox、Button、Alert |
| 通知投递 | Card、Switch、Form、Input、Divider、Checkbox、Alert、Statistic、List |
| 接入密钥 | Form、Select、Checkbox.Group、Table、Popconfirm、Button、Alert |
| 账号与角色 | Table、服务端分页、Select、Radio.Group、Form、Card、List |
| 系统审计 | Form、Input、Table、分页与可展开详情 |

清理约 900 行历史原生控件及资源组件 CSS，保留布局所需样式，不再用全局 input/button/table/label 规则覆盖组件库。中文 locale 改为 ESM 导入，分页与空状态中文正常；按钮关闭自动插入空格。CSP nonce 与错误边界保留，图标继续使用公开具名导入。业务页面不再使用原生 input/select/textarea/button/form/table/details；只有错误边界保留原生恢复按钮，避免组件库自身异常时无法重新加载。

## 验证

- TypeScript/Vite 构建、Go 相关测试、go vet、diff 检查通过。构建中的 use-client 提示与包体大小警告不影响静态 React 应用运行；资源全部嵌入 Go，不依赖运行时 CDN。
- 模拟浏览器完成账号登录、七个模块切换、资源生命周期页签、账号编辑表单、所属账号选择、55 容器服务端分页和跨页勾选保留；节点设置和通知配置表单提交验证成功。测试只使用回环模拟环境，不操作业务 Docker。
- 小屏 390px 通知配置页面验证无整体横向溢出；测试后恢复默认浏览器尺寸。
- 正式 18086 地址实际使用 admin 登录，服务数据加载正常；检查全部七个模块和节点设置弹窗、通知地址/鉴权配置。浏览器无运行错误。正式环境未提交台账/通知/角色变更，未签发、轮换或停用 Key，未执行重启。
- pgverify 验证账号登录、12 项 Agora 权限、2 类角色、9 个节点、2 个任务、4 个重启 Key 和通知存储正常，台账版本保持 1aeebba24378ee0b24dfe0a3603d544e。

正式资源：index-MP52qRuu.js、index-Qx3-eAkS.css。中心更新前确认无活动任务，旧程序保存在独立 `/var/backups/avops-antd-complete.*` 目录。仅重启监控中心，没有停止/重启 RTC 业务容器；中心来源 IP 白名单按用户要求继续关闭，账号、TLS、Agent 身份和重启安全控制保留。
