# 离线交付

本目录的 `monitor-1.0.4-linux-amd64.tar.gz` 使用Git LFS保存，包含中心/Web、Agent、PostgreSQL 17.11、cAdvisor 0.60.6、通知接收服务全部镜像和安装材料。使用 `git lfs pull` 下载真实文件，再按同目录SHA256校验。

解压后查看README.md，或阅读仓库内monitor/deploy/OFFLINE-INSTALL.md。版本说明见monitor/RELEASE-NOTES-1.0.4.md，干净安装结果见同目录验收记录。包内不含证书、私钥、密码、Token或客户运行数据。
