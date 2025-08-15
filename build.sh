#!/bin/bash

set -euo pipefail

# 获取脚本所在目录（兼容软链接）
workdir=$(cd "$(dirname "${BASH_SOURCE[0]}")" &>/dev/null && pwd -P)

# 使用UNIX时间戳作为版本号（跨平台兼容）
version=${1:-$(date +%s)}

# 设置构建参数
GOOS=${GOOS:-$(go env GOOS)}
GOARCH=${GOARCH:-$(go env GOARCH)}
bin_name="polaris-sidecar"
[[ "$GOOS" == "windows" ]] && bin_name="polaris-sidecar.exe"

folder_name="polaris-sidecar-release_${version}.${GOOS}.${GOARCH}"
pkg_name="${folder_name}.zip"

echo "GOOS is ${GOOS}, GOARCH is ${GOARCH}, binary name is ${bin_name}"

cd "$workdir" || exit 1

# 清理旧构建
rm -rf "${folder_name}" "${pkg_name}" "${pkg_name}.md5sum" || true

# 编译二进制
export CGO_ENABLED=0
build_date=$(date "+%Y%m%d.%H%M%S")
package="github.com/polarismesh/polaris-sidecar/version"

GOARCH=${GOARCH} GOOS=${GOOS} go build -o "${bin_name}" \
  -ldflags="-X ${package}.Version=${version} -X ${package}.BuildDate=${build_date}" || exit 1

chmod +x "${bin_name}"

# 创建发布目录
mkdir -p "${folder_name}" || exit 1
cp "${bin_name}" polaris-sidecar.yaml "${folder_name}/" || exit 1
cp -r tool "${folder_name}/" || exit 1

# 压缩打包（-j参数去除目录结构）
zip -r "${pkg_name}" "${folder_name}" || exit 1

# 生成校验和
if command -v md5sum &>/dev/null; then
  md5sum "${pkg_name}" > "${pkg_name}.md5sum"
elif command -v md5 &>/dev/null; then
  md5 -r "${pkg_name}" > "${pkg_name}.md5sum"
else
  echo "Warning: md5sum/md5 command not found, skipping checksum generation"
fi

# 清理临时文件
rm -rf "${folder_name}"

echo "Build successful: ${pkg_name}"
exit 0  # 确保返回成功状态