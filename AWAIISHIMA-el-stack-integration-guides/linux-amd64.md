# Linux AMD64 向け el-stack 組み込み手順

<!-- AWAIISHIMA.md の linux_amd64 向け手順を移行した文書です。TODO は元文書に記載がない項目です。 -->

## 概要

Linux AMD64 向けに el-stack のスタティックライブラリをビルドし、geth に組み込む手順を示す。

最終更新日: 2026/05/14

## 動作確認環境

この手順の作成者が確認に使用した環境は次のとおり。

- CPU アーキテクチャ: x86_64（AMD64）
- OS: Windows 11 Pro 25H2（OS ビルド 26200.8457）上の WSL2
- WSL ディストリビューション: Ubuntu 24.04

<!-- TODO: Rust、Go、C コンパイラのバージョンを記載する。 -->

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
./generate_go.sh
```

## 生成物の確認

<!-- TODO: generate_go.sh が生成するファイルの名前、形式、出力先、確認コマンドを記載する。 -->
成果物の出力先: 
`el-stack-rs/target/release/libel_stack.a`

## geth への配置

ビルドで生成したスタティックライブラリを geth 側へ配置し、geth からリンクできる状態にする。  
(採用しているel-stackのバージョンの判別が可能なよう、下記に示す配置のようにファイル名にバージョンを含めること)

<!-- TODO: geth 内の配置先と、必要なヘッダーファイルなどを記載する。 -->
例: el-stack version 1.0.0を利用する場合  
`libel_stack_v1.0.0.a`

libel_stack.aの配置先: 
`go-ethereum/p2p/elstack/el_stack/libs/linux_amd64/libel_stack_<version_number>.a`

## geth のビルドとリンク確認
go-ethereum/p2p/elstack/el_stack/el_stack.goの以下の記述を配置したライブラリファイル名と合わせる
```Go
// #cgo !android,linux,amd64 LDFLAGS: ${SRCDIR}/libs/linux_amd64/libel_stack_<version_number>.a -lm
```

<!-- TODO: geth のビルドコマンドと、el-stack がリンクされたことを確認する方法を記載する。 -->
go-ethereumディレクトリに移動したCLIで下記コマンドを実行してgethのビルドを行う
エラーが発生せずに動作が終了すれば、ビルドは成功
```
make all
```

## トラブルシューティング

<!-- TODO: 既知の問題、エラー例、解決方法を記載する。 -->
