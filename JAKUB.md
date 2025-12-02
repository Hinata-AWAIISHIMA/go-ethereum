# Jakub MEMOs

1. [Merged updates from upstream](#merged-updates-from-upstream)
2. [Building mobile geth library with patched go1.22.12 compiler](#building-mobile-geth-library-with-patched-go12212-compiler)

## Merged updates from upstream

### Package `cmd/bootnode`:

- Merged updates from upstream up to [v1.15.0](https://github.com/ethereum/go-ethereum/releases/tag/v1.15.0)
- `cmd/bootnode` removed from upstream at commit [53f66c1b03d5880fa807e088c865b31d144fabd0](https://github.com/ethereum/go-ethereum/commit/53f66c1b03d5880fa807e088c865b31d144fabd0)

### Package `p2p`:

- Merged updates from upstream up to [v1.15.11](https://github.com/ethereum/go-ethereum/releases/tag/v1.15.11)

## Building mobile geth library with patched go1.22.12 compiler

### Install Go compiler binaries for bootstrap

Install go1.20.7 compiler using MacPorts:

```console
git clone --single-branch https://github.com/macports/macports-ports.git
cd macports-ports
git checkout 079f0c0e7d498748f01a461cc3d0ffe04442fa8a
cd lang/go
sudo port install
sudo port installed go
sudo port activate go @1.20.7_0
go version
```

For other Go versions check here:\
https://github.com/macports/macports-ports/commits/master/lang/go

### Install a C compiler

To build a Go installation with cgo support, which permits Go programs to import C libraries, a C compiler such as gcc or clang must be installed first. Do this using whatever installation method is standard on the system. 

```console
clang -v
```

### Enable building with Cgo support

```console
export CGO_ENABLED=1
echo $CGO_ENABLED
```

### Fetch the patched Go repository

```console
git clone https://github.com/fblch/go goroot
cd goroot
git checkout fblch/release-branch.go1.22
```

### Build the patched Go compiler

```console
cd src
sudo ./make.bash
```

### Disable the bootstrap Go compiler

```console
sudo port deactivate go @1.20.7_0
sudo port installed go
```

### Enable the patched Go compiler

Add `goroot/bin` to `PATH`:

```console
vi ~/.bash_profile
...
# for building mobile geth library with patched go1.22.12 compiler
export PATH="$PATH:/Users/Jakub/Projects/DEVELOPMENT/goroot/bin"
...
:wq!
```

Load the changes:

```console
source ~/.bash_profile
```

Confirm:

```console
go version
```

### Fetch the geth repository

```console
git clone https://github.com/fblch/go-ethereum
cd go-ethereum
git checkout fblch
```

### Build mobile geth library for Android

Requires NDK r28 or higher for 16 KB page size support:\
https://developer.android.com/guide/practices/page-sizes#compile-r28

```console
export ANDROID_HOME=/Users/Jakub/Development/adt-bundle-mac-x86_64-20140702/sdk
make android
```

### Build mobile geth library for iOS

```console
make ios
```
