#!/usr/bin/env bash
# Fetches github.com/arjun988/kev's source into ./src (gitignored, not
# committed -- it's a third-party project we build against, not something
# this repo vendors) at the commit pinned below, ready for Dockerfile to
# COPY straight in.
#
# Done as a separate step rather than inside the Dockerfile (a plain
# `git clone` in a RUN instruction, which would also work on a normal
# machine with ordinary internet access) so the image build itself needs
# nothing beyond what's already in the node:20-slim base -- no apt-get,
# no installing git into the image just to throw it away after.
set -euo pipefail

KEV_REPO="${KEV_REPO:-https://github.com/arjun988/kev.git}"
# Pinned commit, not a moving branch: this repo has no tagged releases
# yet (see the Dockerfile comment). Check https://github.com/arjun988/kev/tags
# before bumping, in case that's changed.
KEV_REF="${KEV_REF:-959da16ee364722ba06daeac392df96c011c67b4}"

cd "$(dirname "${BASH_SOURCE[0]}")"
rm -rf src
git init -q src
git -C src remote add origin "$KEV_REPO"
git -C src fetch --depth 1 origin "$KEV_REF"
git -C src checkout -q FETCH_HEAD
rm -rf src/.git

echo "fetched arjun988/kev @ $KEV_REF into $(pwd)/src"
