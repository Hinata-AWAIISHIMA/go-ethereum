# Android ARM64 向け el-stack 組み込み手順

<!-- AWAIISHIMA.md の android_arm64 向け手順を移行した文書です。TODO は元文書に記載がない項目です。 -->

## 概要

Linux AMD64 のホスト環境で Android ARM64 向け el-stack のスタティックライブラリをクロスビルドし、cgo を通して geth に組み込む手順を示す。

最終更新日: 2026/05/14

## 動作確認環境

この手順の作成者が確認に使用した環境は次のとおり。

- CPU アーキテクチャ: x86_64（AMD64）
- OS: Windows 11 Pro 25H2（OS ビルド 26200.8457）上の WSL2
- WSL ディストリビューション: Ubuntu 24.04
- Android SDK Platform: API レベル 36（36.0.0および36.1.0）
- Android NDK: 28.1.13356709（r28b）
- ターゲット ABI: Android ARM64

<!-- TODO: Rust、Go、JDK、Android SDK Command-line Tools の実際のバージョンを記載する。 -->

## 前提条件

el-stack は Rust で記述されているため、ビルド前に Rust を利用できる状態にしておく。

TheChainApp のビルドで使用しているものと同じ Android SDK および NDK を用意する。この手順では、Windows 側の SDK/NDK を WSL2 から参照せず、Ubuntu 側へインストールする。

### 必要パッケージのインストール

```bash
sudo apt update
sudo apt install -y wget unzip openjdk-17-jdk
```

### Android SDK Command-line Tools の配置

```bash
mkdir -p ~/Android/Sdk
cd ~/Android
wget https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip
unzip commandlinetools-linux-*_latest.zip
mkdir -p ~/Android/Sdk/cmdline-tools
mv cmdline-tools ~/Android/Sdk/cmdline-tools/latest
```

### SDK の環境変数設定

次の設定を `~/.bashrc` へ追加し、現在のシェルへ反映する。

```bash
echo 'export ANDROID_HOME=$HOME/Android/Sdk' >> ~/.bashrc
echo 'export PATH=$ANDROID_HOME/cmdline-tools/latest/bin:$PATH' >> ~/.bashrc
echo 'export PATH=$ANDROID_HOME/platform-tools:$PATH' >> ~/.bashrc
echo 'export PATH=$ANDROID_HOME/emulator:$PATH' >> ~/.bashrc
source ~/.bashrc
```

`sdkmanager` が実行できることを確認する。

```bash
sdkmanager --version
```

### SDK ライセンスへの同意

```bash
yes | sdkmanager --licenses
```

### Android SDK Platform のインストール

Android Platform 36.0.0をインストールする。

```bash
sdkmanager "platforms;android-36"
```

Android Platform 36.1.0をインストールする。

```bash
sdkmanager "platforms;android-36-ext1"
```

### Android NDK のインストール

```bash
sdkmanager "ndk;28.1.13356709"
```

頻繁に Android 向けビルドを行う場合は、任意で NDK の環境変数も `~/.bashrc` に追加する。

```bash
echo 'export ANDROID_NDK_HOME=$ANDROID_HOME/ndk/28.1.13356709' >> ~/.bashrc
echo 'export PATH=$PATH:$ANDROID_NDK_HOME' >> ~/.bashrc
source ~/.bashrc
```

### Rust target の追加

```bash
rustup target add aarch64-linux-android
```

## el-stack のソース取得

el-stack のリポジトリをクローンし、作業ディレクトリへ移動する。

```bash
git clone https://github.com/freebit-rd/el-stack-rs.git
cd el-stack-rs
```

<!-- TODO: 動作確認済みのブランチまたはコミットを記載する。 -->

## el-stack のビルド

### NDK とツールチェーンの環境変数設定

```bash
export ANDROID_NDK_HOME="$HOME/Android/Sdk/ndk/28.1.13356709"
export NDK_ROOT="$ANDROID_NDK_HOME"
export TOOLCHAIN_BIN="$NDK_ROOT/toolchains/llvm/prebuilt/linux-x86_64/bin"
```

### Android向けクロスコンパイル環境変数の設定

```bash
export CC_aarch64_linux_android="$TOOLCHAIN_BIN/aarch64-linux-android21-clang"
export AR_aarch64_linux_android="$TOOLCHAIN_BIN/llvm-ar"
export RANLIB_aarch64_linux_android="$TOOLCHAIN_BIN/llvm-ranlib"
export RANLIB="$RANLIB_aarch64_linux_android"
export CARGO_TARGET_AARCH64_LINUX_ANDROID_LINKER="$CC_aarch64_linux_android"
```

### ライブラリのビルド

```bash
cargo build \
    --package "el_stack" \
    --release \
    --target aarch64-linux-android
```

## 生成物の確認

生成物は `target/aarch64-linux-android/release` に出力される。

<!-- TODO: 生成物の正確なファイル名と、Android ARM64 向けバイナリであることの確認コマンドを記載する。 -->

## geth への配置

ビルドで生成したスタティックライブラリを geth 側へ配置し、geth からリンクできる状態にする。  
採用している el-stack のバージョンを判別できるよう、配置するファイル名にバージョンを含めること。

配置先は次のとおり。`<version_number>` は採用する el-stack のバージョン番号へ置き換える。

例: el-stack version 1.0.0を利用する場合  
`libel_stack_v1.0.0.a`

```text
go-ethereum/p2p/elstack/el_stack/libs/android_arm64/libel_stack_<version_number>.a
```

## geth のビルドとリンク確認

`go-ethereum/p2p/elstack/el_stack/el_stack.go` の次の記述を、配置したライブラリのファイル名と一致させる。

```go
// #cgo android,arm64 LDFLAGS: ${SRCDIR}/libs/android_arm64/libel_stack_<version_number>.a -lm
```

`go-ethereum` ディレクトリへ移動し、Android ARM64 向け geth をビルドする。
android向けgethのビルド手順は本ドキュメントでは詳細に記さない。

```bash
make android
```

エラーが発生せずにコマンドが終了し、`go-ethereum/build/bin/geth.aar` と `go-ethereum/build/bin/geth-sources.jar` が生成されれば、ビルドは成功である。

## トラブルシューティング

<!-- TODO: NDK、API レベル、cgo に関する既知の問題、エラー例、解決方法を記載する。 -->
