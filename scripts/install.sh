#!/bin/bash
set -e

# 配置仓库信息
REPO="1024XEngineer/neo-code" 
PROJECT_NAME="neocode"
BINARY_NAME="neocode"

echo "🚀 开始安装 $BINARY_NAME..."

# 1. 获取系统和架构信息
OS="$(uname -s)"
ARCH="$(uname -m)"

if [ "$OS" = "Linux" ]; then
    OS_NAME="Linux"
elif [ "$OS" = "Darwin" ]; then
    OS_NAME="Darwin"
else
    echo "❌ 不支持的操作系统: $OS"
    exit 1
fi

if [ "$ARCH" = "x86_64" ] || [ "$ARCH" = "amd64" ]; then
    ARCH_NAME="x86_64"
elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
    ARCH_NAME="arm64"
else
    echo "❌ 不支持的系统架构: $ARCH"
    exit 1
fi

# 2. 从 GitHub API 获取最新 Release 版本号
echo "🔍 正在获取最新版本信息..."
LATEST_TAG=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST_TAG" ]; then
    echo "❌ 无法获取最新版本，请检查网络或 GitHub 访问权限。"
    exit 1
fi
echo "📦 发现最新版本: $LATEST_TAG"

# 3. 拼接下载链接 (匹配 GoReleaser 默认命名)
TAR_FILE="${PROJECT_NAME}_${OS_NAME}_${ARCH_NAME}.tar.gz"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$LATEST_TAG/$TAR_FILE"

# 4. 下载并解压
echo "⬇️  正在下载: $DOWNLOAD_URL"
curl -L -o "$TAR_FILE" "$DOWNLOAD_URL"

echo "📦 正在解压..."
tar -xzf "$TAR_FILE" "$BINARY_NAME"

# 5. 安装到全局 PATH
echo "⚙️  正在安装到 /usr/local/bin (此步可能需要输入密码以获取 sudo 权限)..."
sudo mv "$BINARY_NAME" /usr/local/bin/

# 6. 清理临时文件
rm "$TAR_FILE"

echo "✅ 安装成功！请在终端运行 '$BINARY_NAME --help' 开始使用。"
