# 开发指南
## 代码提交前

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