# Linux ARM64 向け el-stack 組み込み手順

<!-- AWAIISHIMA.md の linux_arm64 向け手順を移行した文書です。TODO は元文書に記載がない項目です。 -->

## 概要

Linux AMD64 のホスト環境で Linux ARM64 向け el-stack のスタティックライブラリをクロスビルドし、geth に組み込む手順を示す。

最終更新日: 2026/05/14

## 動作確認環境

この手順の作成者が確認に使用したホスト環境は次のとおり。

- CPU アーキテクチャ: x86_64（AMD64）
- OS: Windows 11 Pro 25H2（OS ビルド 26200.8457）上の WSL2
- WSL ディストリビューション: Ubuntu 24.04
- ターゲットアーキテクチャ: Linux ARM64

<!-- TODO: Rust、Go、AArch64 GNU ツールチェーンのバージョンを記載する。 -->

## 前提条件

el-stack は Rust で記述されているため、ビルド前に Rust を利用できる状態にしておく。

### クロスコンパイルツールのインストール

```bash
sudo apt update
sudo apt install gcc-aarch64-linux-gnu binutils-aarch64-linux-gnu libc6-dev-arm64-cross
```

### Rust target の追加

```bash
rustup target add aarch64-unknown-linux-gnu
```

## el-stack のソース取得

el-stack のリポジトリをクローンし、作業ディレクトリへ移動する。

```bash
git clone https://github.com/freebit-rd/el-stack-rs.git
cd el-stack-rs
```

<!-- TODO: 動作確認済みのブランチまたはコミットを記載する。 -->

## el-stack のビルド

### クロスコンパイル用環境変数の設定

```bash
export CC_aarch64_unknown_linux_gnu="aarch64-linux-gnu-gcc"
export AR_aarch64_unknown_linux_gnu="aarch64-linux-gnu-ar"
export CARGO_TARGET_AARCH64_UNKNOWN_LINUX_GNU_LINKER="aarch64-linux-gnu-gcc"
```

### ライブラリのビルド

```bash
cargo build --package el-stack --features loginit --release --target aarch64-unknown-linux-gnu
```

## 生成物の確認

<!-- TODO: 生成物の正確なファイル名、出力先、ARM64 バイナリであることの確認コマンドを記載する。 -->
`el-stack-rs/target/aarch-unknown-linux-gnu/release/libel_stack.a`

## geth への配置

ビルドで生成したスタティックライブラリを geth 側へ配置し、geth からリンクできる状態にする。  
採用している el-stack のバージョンを判別できるよう、配置するファイル名にバージョンを含めること。

配置先は次のとおり。`<version_number>` は採用する el-stack のバージョン番号へ置き換える。

例: el-stack version 1.0.0を利用する場合  
`libel_stack_v1.0.0.a`

```text
go-ethereum/p2p/elstack/el_stack/libs/linux_arm64/libel_stack_<version_number>.a
```

## geth のビルドとリンク確認

`go-ethereum/p2p/elstack/el_stack/el_stack.go` の次の記述を、配置したライブラリのファイル名と一致させる。

```go
// #cgo !android,linux,arm64 LDFLAGS: ${SRCDIR}/libs/linux_arm64/libel_stack_<version_number>.a -lm
```

`go-ethereum` ディレクトリへ移動し、次のコマンドで Linux ARM64 向け geth をクロスビルドする。

```bash
go run build/ci.go install -arch arm64 -cc aarch64-linux-gnu-gcc
```

エラーが発生せずにコマンドが終了すれば、ビルドは成功である。

## トラブルシューティング

<!-- TODO: クロスコンパイル時の既知の問題、エラー例、解決方法を記載する。 -->
