# 开发指南
## 代码提交前置步骤
### 第一步：语法检查
```shell
make lint
```
### 第二步：格式化
```shell
make fmt
```
### 第三步：提交到远程
- 提交内容格式为 {type}:{content}
```shell
git add .
git commit -m "feat: add some feature"
git push
```

## 本地自测
- 参数设置, 根据需要修改
```shell
# 打包版本
export VERSION=v2.1.0
# 北极星服务地址
export POLARIS_ADDRESS=127.0.0.1:8091
```
### 编译
```shell
make clean
make build
```
### 部署
- 进入 release 包，以 mac为例
```shell
cd polaris-sidecar-release_${VERSION}.darwin.arm64
```
- 启动 polaris-sidecar 进程
```shell
bash tool/start.sh
```
- 查看 polaris-sidecar 进程
```shell
bash tool/p.sh
```
- 停止 polaris-sidecar 进程
```shell
bash tool/stop.sh
```
### 验证
- dig
```shell
dig polaris.checker.polaris
```
- nslookup
```shell
nslookup polaris.checker.polaris
```
