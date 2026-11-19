# 远端 Docker 构建与 Web 预览

`docker-test-remote.ps1` 会把当前工作区中已跟踪和未忽略的文件打包，通过 SSH 上传到测试服务器，再由服务器上的 Docker Compose 构建并启动项目。构建会执行 Dockerfile 中的前端生产构建，预览的是镜像内的静态前端资源。

## 配置并启动

在 PowerShell 中设置 SSH 目标并运行脚本。目标格式为 `SSH用户名@ssh.instabox.sh`；SSH 端口默认是 `2222`。

```powershell
$env:LINAI_TEST_SSH_TARGET = "SSH用户名@ssh.instabox.sh"
.\deploy\docker-test-remote.ps1
```

脚本会在远端启动独立的 Compose 项目，将 `0.0.0.0:3001` 映射到应用容器的 `8080` 端口，检查 `/health`，再发布公网 HTTPS 预览 URL。成功后访问脚本输出的 `PREVIEW_URL`；任何知道此 URL 的人都能在测试服务运行期间访问。脚本也会输出 `LOGIN_EMAIL` 和 `LOGIN_PASSWORD`；它们保存在服务器用户目录下的配置文件中，重建测试容器时保持不变。

SSH 首次连接时会接受并保存服务器主机密钥。脚本不会把 SSH 目标或远端数据库密钥写入仓库；测试用数据库/JWT 密钥保存在服务器用户目录下的 `~/.config/linai-api-docker-test.env`，权限为 `600`。业务数据保存在独立的 Docker 命名卷中。

## 停止测试服务

```powershell
.\deploy\docker-test-remote.ps1 -Stop
```

`-Stop` 会撤销公网预览地址，停止并移除测试容器及网络，保留测试数据卷，以便再次启动时继续使用。测试服务再次启动时会基于最新工作区重建镜像。

本地直接运行 `docker-compose.dev.yml` 的方式仍可用于本机 Docker 测试；远端测试用 `docker-compose.remote-test.yml` 覆盖数据卷配置，避免依赖本机文件路径。
