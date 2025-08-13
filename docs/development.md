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
#### 验证请求北极星的域名
- 使用nslookup
```shell
nslookup polaris.checker.polaris
# recurse 开启和关闭时均成功
nslookup www.baidu.com
```
- 通过 SRV 类型获取 IP 和端口
```shell
dig SRV polaris.checker.polaris +short | awk '{print $4 ":" $3}' | while read line; do host=${line%:*}; port=${line#*:}; dig +short $host | awk -v p=$port '{print $1 ":" p}'; done
```
#### 验证请求本地 nameserver的所有类型
- 观察是否全部都是 NOERROR
```shell
bash test/run_dns_queries.sh
```