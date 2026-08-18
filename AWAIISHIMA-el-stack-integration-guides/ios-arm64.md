# iOS / iOS Simulator ARM64 向け el-stack 組み込み手順

<!-- Linux AMD64 向け手順を基に、iOS実機およびiOS Simulator ARM64向けの情報へ置き換えた文書です。 -->

## 概要

macOS のホスト環境で、iOS ARM64 実機およびiOS Simulator ARM64向け el-stack のスタティックライブラリをビルドし、geth に組み込む手順を示す。

geth のiOS向けビルドでは、iOS実機向けとiOS Simulator向けのビルドが同時に実行されるため、両ターゲットの手順をこの文書でまとめて扱う。

最終更新日: 2026/07/23

## 動作確認環境

この手順の作成者が確認に使用したホスト環境は次のとおり。

- CPU アーキテクチャ: ARM64
- OS: macOS Sequoia 15.4.1
- ターゲット:
  - iOS ARM64 実機
  - iOS Simulator ARM64

<!-- TODO: Rust、Go、Xcode、iOS SDK、iOS Simulator SDK、Deployment Target のバージョンを記載する。 -->

## 前提条件

el-stack は Rust で記述されているため、ビルド前に Rust を利用できる状態にしておく。

iOS 向けフレームワークのビルドに使用する Xcode、iOS SDK、iOS Simulator SDK を利用できる状態にしておく。

<!-- TODO: 動作確認済みのXcode、各SDK、Rust targetなどの詳細を記載する。 -->

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
./generate_ios.sh
```

## 生成物の確認

各成果物の出力先は下記の通りである。
`ios: el-stack-rs/target/aarch64-apple-ios/release/libel_stack.a`
`iossimulator: el-stack-rs/target/aarch64-apple-ios-sim/release/libel_stack.a`

## geth への配置

ビルドで生成したスタティックライブラリを geth 側へ配置し、geth からリンクできる状態にする。  
採用している el-stack のバージョンを判別できるよう、配置するファイル名にバージョンを含めること。

配置先はターゲットごとに次のとおり。`<version_number>` は採用する el-stack のバージョン番号へ置き換える。

例: el-stack version 1.0.0を利用する場合  
`libel_stack_v1.0.0.a`

iOS ARM64実機向け:

```text
ios: go-ethereum/p2p/elstack/el_stack/libs/ios_arm64/libel_stack_<version_number>.a
iossimulator: go-ethereum/p2p/elstack/el_stack/libs/iossimulator_arm64/libel_stack_<version_number>.a
```

iOS Simulator ARM64向け:

```text
go-ethereum/p2p/elstack/el_stack/libs/iossimulator_arm64/libel_stack_<version_number>.a
```

## geth のビルドとリンク確認

`go-ethereum/p2p/elstack/el_stack/el_stack.go` にあるiOS実機用とiOS Simulator用の記述を、それぞれ配置したライブラリのファイル名と一致させる。

```go
// #cgo ios,arm64 LDFLAGS: ${SRCDIR}/libs/ios_arm64/libel_stack_<version_number>.a -lm
// #cgo iossimulator,arm64 LDFLAGS: ${SRCDIR}/libs/iossimulator_arm64/libel_stack_<version_number>.a -lm
```

`go-ethereum` ディレクトリへ移動し、次のコマンドでiOS実機およびiOS Simulator向けフレームワークを同時にビルドする。

```bash
make ios
```

エラーが発生せずにコマンドが終了し、`go-ethereum/build/bin/Geth.framework` が生成されれば、ビルドは成功である。
実際にiOSアプリに組み込む場合の手順は本ドキュメントに掲載しない

## トラブルシューティング

<!-- TODO: iOS SDK、iOS Simulator SDK、Deployment Target、コード署名、cgo、実機用とSimulator用ライブラリの取り違えに関する問題と解決方法を記載する。 -->
