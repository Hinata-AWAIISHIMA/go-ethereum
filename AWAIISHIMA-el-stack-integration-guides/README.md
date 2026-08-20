# el-stack integration guides for geth

このディレクトリは、el-stack を geth に組み込むための手順を対象アーキテクチャごとに管理する。

ルートの [`AWAIISHIMA.md`](../AWAIISHIMA.md) に記載されている手順を、該当するアーキテクチャの手順書へ移行している。元文書に記載がない項目は TODO として残しているため、手順の整備が完了するまでは `AWAIISHIMA.md` も併せて参照すること。

`AWAIISHIMA.md` が対象としているのは Linux AMD64、Linux ARM64、Android ARM64、Windows AMD64 である。macOS ARM64、iOS ARM64、iOS Simulator ARM64 については、Linux AMD64 向け手順を基に、確認済みのビルド環境とリポジトリ内の配置・リンク設定を反映している。iOS実機向けとiOS Simulator向けは同時にビルドされるため、一つの手順書で扱う。

## 対象アーキテクチャ

| アーキテクチャ | 手順書 |
| --- | --- |
| Linux AMD64 | [`linux-amd64.md`](linux-amd64.md) |
| Linux ARM64 | [`linux-arm64.md`](linux-arm64.md) |
| Android ARM64 | [`android-arm64.md`](android-arm64.md) |
| Windows AMD64 | [`windows-amd64.md`](windows-amd64.md) |
| macOS ARM64 | [`macos-arm64.md`](macos-arm64.md) |
| iOS / iOS Simulator ARM64 | [`ios-arm64.md`](ios-arm64.md) |

## 各手順書の構成

アーキテクチャ間で比較しやすく、前提条件の確認からリンク結果の検証までを同じ順序で追えるように、各手順書は次の構成に統一する。

1. 概要
2. 動作確認環境
3. 前提条件
4. el-stack のソース取得
5. el-stack のビルド
6. 生成物の確認
7. geth への配置
8. geth のビルドとリンク確認
9. トラブルシューティング

## 分割作業の方針

- 各手順書だけで対象アーキテクチャの作業を完結できるようにし、参照漏れを防ぐ。
- ツール、SDK、NDK などは動作確認済みのバージョンを明記し、バージョン差による問題を切り分けられるようにする。
- コマンドを実行する OS とシェルを明記し、シェル構文やホスト用ツールの取り違えを防ぐ。
- 生成物のファイル名、生成先、geth 内の配置先を明記し、異なるターゲットやビルド構成のライブラリを誤ってリンクすることを防ぐ。
- 各コマンドには、根拠が確認できる範囲で目的または必要な理由を添える。未確認の内容は断定せず、TODO または注意事項として明示する。
- 共通手順であっても、読者が別文書を往復せずに済む範囲で各手順書に記載する。
