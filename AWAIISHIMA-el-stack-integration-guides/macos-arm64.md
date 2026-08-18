# macOS ARM64 向け el-stack 組み込み手順

<!-- Linux AMD64 向け手順を基に、macOS ARM64 向けの情報へ置き換えた文書です。 -->

## 概要

Apple Silicon を搭載した macOS ARM64 向けに el-stack のスタティックライブラリをビルドし、geth に組み込む手順を示す。

最終更新日: 2026/07/23

## 動作確認環境

この手順の作成者が確認に使用した環境は次のとおり。

- CPU アーキテクチャ: ARM64
- OS: macOS Sequoia 15.4.1

<!-- TODO: Rust、Go、Xcode、Command Line Tools、C コンパイラのバージョンを記載する。 -->

## 前提条件

el-stack は Rust で記述されているため、ビルド前に Rust を利用できる状態にしておく。

このアーキテクチャに関する追加の前提条件はない。

## el-stack のソース取得

el-stack のリポジトリをクローンし、作業ディレクトリへ移動する。

```bash
git clone https://github.com/freebit-rd/el-stack-rs.git
cd el-stack-rs
```

<!-- TODO: 動作確認済みのブランチまたはコミットを記載する。 -->

## el-stack のビルド

リポジトリのルートで生成スクリプトを実行する。

```bash
./generate_macos.sh
```

## 生成物の確認

成果物の出力先は `el-stack-rs/target/aarch64-apple-darwin/release/libel_stack.a` である。

## geth への配置

ビルドで生成したスタティックライブラリを geth 側へ配置し、geth からリンクできる状態にする。  
採用している el-stack のバージョンを判別できるよう、配置するファイル名にバージョンを含めること。

配置先は次のとおり。`<version_number>` は採用する el-stack のバージョン番号へ置き換える。

例: el-stack version 1.0.0を利用する場合  
`libel_stack_v1.0.0.a`

```text
go-ethereum/p2p/elstack/el_stack/libs/darwin_arm64/libel_stack_<version_number>.a
```

## geth のビルドとリンク確認

`go-ethereum/p2p/elstack/el_stack/el_stack.go` の次の記述を、配置したライブラリのファイル名と一致させる。

```go
// #cgo darwin,arm64 LDFLAGS: ${SRCDIR}/libs/darwin_arm64/libel_stack_<version_number>.a -lm -framework SystemConfiguration -framework CoreFoundation
```

`go-ethereum` ディレクトリへ移動し、次のコマンドで geth をビルドする。

```bash
make all
```

エラーが発生せずにコマンドが終了すれば、ビルドは成功である。

## トラブルシューティング

<!-- TODO: Apple SDK、コード署名、cgo、アーキテクチャ不一致に関する問題と解決方法を記載する。 -->
