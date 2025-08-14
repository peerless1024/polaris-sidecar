#!/bin/bash

set -euo pipefail  # 启用严格模式

# 获取版本号或使用时间戳
if [ $# -gt 0 ]; then
  version="$1"
else
  version=$(date +%s)  # 简化时间戳生成
fi

# 获取工作目录
workdir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)  # 更可靠的路径获取方式
bin_name="polaris-sidecar"

# 设置GO环境变量
GOOS=${GOOS:-$(go env GOOS)}
GOARCH=${GOARCH:-$(go env GOARCH)}

echo "🚀 开始构建版本 ${version} (${GOOS}/${GOARCH})"

# 构建部署包
if ! bash build.sh "${version}"; then
  echo "❌ 构建失败" >&2
  exit 1
fi

package_name="polaris-sidecar-local_${version}.${GOOS}.${GOARCH}.zip"
folder_name="polaris-sidecar-install"

# 创建目录并移动文件
mkdir -p "${folder_name}" || exit 1

# 使用通配符查找部署包
deploy_packages=(polaris-sidecar-release_*.zip)
if [ ${#deploy_packages[@]} -eq 0 ]; then
  echo "❌ 未找到部署包" >&2
  exit 1
elif [ ${#deploy_packages[@]} -gt 1 ]; then
  echo "⚠️  找到多个部署包，使用最新版本"
  latest_package=$(ls -t polaris-sidecar-release_*.zip | head -1)
  mv "${latest_package}" "${folder_name}/"
else
  mv "${deploy_packages[0]}" "${folder_name}/"
fi

# 复制部署脚本
cp ./deploy/vm/*.sh "${folder_name}/"

# 创建ZIP包
if ! zip -r "${package_name}" "${folder_name}"; then
  echo "❌ 创建ZIP包失败: ${package_name}" >&2
  exit 1
fi

# 清理临时文件
rm -rf "${folder_name}" polaris-sidecar-release_*

echo "✅ 构建完成: ${package_name}"
exit 0  # 确保返回成功状态
