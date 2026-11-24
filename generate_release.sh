#!/bin/sh
bazel build :windows-release :linux-release
VERSION=$(cat ./VERSION)
mkdir release
mv bazel-bin/tisane-setup.exe release/tisane-$VERSION-setup.exe
mv bazel-bin/linux-release.tar.gz release/linux-$VERSION.tar.gz
