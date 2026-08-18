# Windows AMD64 向け el-stack 組み込み手順

<!-- AWAIISHIMA.md の windows_amd64 向け手順を移行した文書です。TODO は元文書に記載がない項目です。 -->

## 概要

Windows AMD64 向けに el-stack のスタティックライブラリを GNU ツールチェーンでビルドし、geth に組み込む手順を示す。

最終更新日: 2026/05/14

## 動作確認環境

この手順の作成者が確認に使用した環境は次のとおり。

- CPU アーキテクチャ: x86_64（AMD64）
- OS: Windows 11 Pro 25H2
- OS ビルド: 26200.8457
- 操作シェル: PowerShellおよびMSYS2 UCRT64
- ターゲット: `x86_64-pc-windows-gnu`

<!-- TODO: MSYS2、Rust、Go、GNU ツールチェーンのバージョンを記載する。 -->

## 前提条件

el-stack は Rust で記述されているため、ビルド前に Rust を利用できる状態にしておく。

### MSYS2 の確認

MSYS2 がインストール済みであることを確認する。標準的なインストール先は `C:\msys64` である。

PowerShellで次のコマンドを実行し、MSYS2 UCRT64 の GNU ツールチェーンに必要な実行ファイルがあることを確認する。

```powershell
Test-Path C:\msys64\ucrt64\bin\gcc.exe
Test-Path C:\msys64\ucrt64\bin\ar.exe
Test-Path C:\msys64\ucrt64\bin\dlltool.exe
```

### MSYS2 の更新

MSYS2 UCRT64 シェルで次のコマンドを実行する。

```bash
pacman -Syu
```

必要に応じてMSYS2を再起動し、更新を続行する。

```bash
pacman -Su
```

### GNU ツールチェーンのインストール

MSYS2 UCRT64 シェルで次のコマンドを実行する。

```bash
pacman -S --needed mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-binutils
```

## el-stack のソース取得

el-stack のリポジトリをクローンする。

```powershell
git clone https://github.com/freebit-rd/el-stack-rs.git
```

PowerShellで作業ディレクトリへ移動する。`<ユーザー名>` は実際のWindowsユーザー名へ置き換える。

```powershell
Set-Location C:\Users\<ユーザー名>\el-stack-rs
```

<!-- TODO: 動作確認済みのブランチまたはコミットを記載する。 -->

## el-stack のビルド

### GNU ツールチェーンをPATHへ追加

```powershell
$env:PATH = "C:\msys64\ucrt64\bin;$env:PATH"
```

### ビルド用環境変数の設定

```powershell
$env:CC = "gcc"
$env:AR = "ar"
$env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER = "gcc"
```

### ツールの確認

```powershell
where.exe gcc
where.exe ar
where.exe dlltool
```

期待される出力例は次のとおり。

```text
C:\msys64\ucrt64\bin\gcc.exe
C:\msys64\ucrt64\bin\ar.exe
C:\msys64\ucrt64\bin\dlltool.exe
```

### ライブラリのビルド

```powershell
cargo build --package el-stack --features loginit --release --target x86_64-pc-windows-gnu
```

## 生成物の確認

次のコマンドでスタティックライブラリが生成されたことを確認する。ファイルエクスプローラーから直接確認してもよい。

```powershell
Get-ChildItem .\target\x86_64-pc-windows-gnu\release\libel_stack.a
```

## geth への配置

ビルドで生成したスタティックライブラリを geth 側へ配置し、geth からリンクできる状態にする。  
採用している el-stack のバージョンを判別できるよう、配置するファイル名にバージョンを含めること。

配置先は次のとおり。`<version_number>` は採用する el-stack のバージョン番号へ置き換える。

例: el-stack version 1.0.0を利用する場合  
`libel_stack_v1.0.0.a`

```text
go-ethereum/p2p/elstack/el_stack/libs/windows_amd64/libel_stack_<version_number>.a
```

## geth のビルドとリンク確認

`go-ethereum/p2p/elstack/el_stack/el_stack.go` の次の記述を、配置したライブラリのファイル名と一致させる。

```go
// #cgo windows,amd64 LDFLAGS: ${SRCDIR}/libs/windows_amd64/libel_stack_<version_number>.a -lm -liphlpapi -luserenv -lntdll
```

`go-ethereum` ディレクトリへ移動した PowerShell で、次のコマンドを実行して geth をビルドする。

```powershell
go run build/ci.go install
```

エラーが発生せずにコマンドが終了すれば、ビルドは成功である。

## トラブルシューティング

<!-- TODO: MSYS2、GNU ABI、PowerShell に関する既知の問題、エラー例、解決方法を記載する。 -->
