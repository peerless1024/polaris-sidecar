#!/bin/bash

set -euo pipefail

# 显示帮助信息
show_help() {
    echo "Usage: $0 [-b dns_back_dir]"
    echo "Options:"
    echo "  -b  DNS configuration backup directory (default: current directory)"
    exit 0
}

# 标准化路径函数
normalize_path() {
    local path="$1"
    # 移除末尾的斜杠
    path="${path%/}"
    # 处理相对路径
    if [[ "$path" != /* ]]; then
        path="$(pwd)/$path"
    fi
    echo "$path"
}

# 默认备份目录为当前目录
DNS_BACK_DIR=$(normalize_path "./")

# 解析命令行参数
while getopts ':b:h' OPT; do
    case "$OPT" in
        b) DNS_BACK_DIR=$(normalize_path "$OPTARG") ;;
        h) show_help ;;
        \?)
            echo "Invalid option: -$OPTARG" >&2
            show_help
            exit 1
            ;;
        :)
            echo "Option -$OPTARG requires an argument." >&2
            show_help
            exit 1
            ;;
    esac
done

echo "[INFO] DNS backup directory: ${DNS_BACK_DIR}"

# 检查sudo权限
check_sudo() {
    if ! sudo -v; then
        echo "[ERROR] This script requires sudo privileges." >&2
        exit 1
    fi
}

# 停止 polaris-sidecar
stop_polaris_sidecar() {
    echo -e "\n[STEP 1] Stopping polaris-sidecar process..."

    local polaris_pkgs=( $(find . -maxdepth 1 -name "polaris-sidecar-release*.zip") )
    if [ ${#polaris_pkgs[@]} -eq 0 ]; then
        echo "[WARN] No polaris-sidecar package found in current directory."
        return 0
    elif [ ${#polaris_pkgs[@]} -gt 1 ]; then
        echo "[ERROR] Multiple polaris-sidecar packages found:" >&2
        printf '  %s\n' "${polaris_pkgs[@]}" >&2
        echo "Please ensure only one package is present." >&2
        return 1
    fi

    local pkg_file="${polaris_pkgs[0]}"
    local extract_dir="${pkg_file%.zip}"

    if [ ! -d "$extract_dir" ]; then
        echo "[INFO] Extracting package: ${pkg_file}..."
        unzip -q "$pkg_file" || {
            echo "[ERROR] Failed to extract package" >&2
            return 1
        }
    fi

    (
        cd "$extract_dir" || {
            echo "[ERROR] Failed to enter directory: $extract_dir" >&2
            exit 1
        }

        echo "[STEP 2] Stopping polaris-sidecar in ${extract_dir}..."
        if [ -f "./tool/stop.sh" ]; then
            chmod +x ./tool/stop.sh
            ./tool/stop.sh || {
                echo "[WARN] stop.sh script returned non-zero status" >&2
            }
        else
            echo "[ERROR] stop.sh script not found in ${extract_dir}/tool/" >&2
            return 1
        fi
    )

    echo "[STEP 3] Verifying polaris-sidecar process stopped..."
    local polaris_sidecar_num
    polaris_sidecar_num=$(pgrep -f polaris-sidecar | wc -l)
    if [ "$polaris_sidecar_num" -ge 1 ]; then
        echo "[WARN] polaris-sidecar is still running. Attempting to kill..."
        pkill -f polaris-sidecar || {
            echo "[ERROR] Failed to stop polaris-sidecar process" >&2
            return 1
        }
    fi

    return 0
}

# 恢复DNS配置
rollback_dns_conf() {
    echo -e "\n[STEP 4] Restoring DNS configuration..."

    if [ ! -d "$DNS_BACK_DIR" ]; then
        echo "[ERROR] Backup directory not found: $DNS_BACK_DIR" >&2
        return 1
    fi

    local last_back_file
    # 安全查找最新备份文件
    last_back_file=""
    while IFS= read -r -d $'\0' file; do
        if [ -z "$last_back_file" ] || [ "$file" -nt "$last_back_file" ]; then
            last_back_file="$file"
        fi
    done < <(find "$DNS_BACK_DIR" -maxdepth 1 -type f -name 'resolv.conf.bak_*' -print0)

    if [ -z "$last_back_file" ]; then
        echo -e "[ERROR] No backup file found in $DNS_BACK_DIR"
        return 1
    fi

    local backup_path="${DNS_BACK_DIR}/${last_back_file}"
    echo "[INFO] Restoring from backup: ${backup_path}"

    check_sudo
    if ! sudo cp -f "$last_back_file" /etc/resolv.conf; then
        echo "[ERROR] Failed to restore /etc/resolv.conf" >&2
        return 1
    fi

    echo "[INFO] Successfully restored DNS configuration."
    return 0
}

# 主卸载流程
main() {
    stop_polaris_sidecar || exit 1
    rollback_dns_conf || exit 1

    echo -e "\n=== Uninstallation Completed ==="
    echo "Polaris Sidecar has been stopped and DNS configuration has been restored."
    exit 0
}

main
