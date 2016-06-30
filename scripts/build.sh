#!/usr/bin/env bash
# 
# Build gatekeeper for release or local development
# This script will output binaries to the ./bin local directory
# 
set -e

# Get the parent directory of this script, which is the `gatekeeper` repo
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ] ; do SOURCE="$(readlink "$SOURCE")"; done
DIR="$( cd -P "$( dirname "$SOURCE" )/.." && pwd )"

cd "$DIR"

#
# Development symlinks the current project into the proper gopath. This is done
# so we can seamlessly reference different packages.
# 
if [[ $GATEKEEPER_DEV = "1" ]];then
  if [[ ! -d "$GOPATH/src/github.com/jonmorehouse/gatekeeper" ]]; then
    echo "symlinking current source into $GOPATH/src/github.com/jonmorehouse/gatekeeper ..."
    mkdir -p "$GOPATH/src/github.com/jonmorehouse"
    ln -sf "$DIR" "$GOPATH/src/github.com/jonmorehouse/gatekeeper"
  fi
fi

# 
# Build packages
#
echo "building shared package ..."
cd "$DIR/shared"
go build .

echo "building plugin/upstream package ..."
cd "$DIR/plugin/upstream"
go build .

echo "building plugin/loadbalancer package ..."
cd "$DIR/plugin/loadbalancer"
go build .

echo "building plugin/event package ..."
cd "$DIR/plugin/metric"
go build .

echo "build plugin/modifier package ..."
cd "$DIR/plugin/modifier"
go build .

echo "building gatekeeper core package ..."
cd "$DIR/gatekeeper"
go build .


# 
# Build the main gatekeeper application
#
if [[ $GATEKEEPER_DEV = "1" ]]; then
  echo "building gatekeeper in dev mode..."
  cd "$DIR"
  go build -o bins/gatekeeper .
  ln -sf "$DIR/bins/gatekeeper" $GOPATH/bin/gatekeeper
else
  echo "building gatekeeper with release settings..."
  exit 0
fi

if [[ $GATEKEEPER_DEV = "1" ]];then
  echo "building each gatekeeper plugin in dev mode..."
  for dir in `find $DIR/plugins -depth 1 -type d`; do
    plugin_name=$(basename $dir)
    echo "building $plugin_name plugin..."
    cd $dir
    go build -o "$DIR/bins/$plugin_name"
    echo "symlinking $plugin_name to $GOPATH/bin/$plugin_name ..."
    ln -sf "$DIR/bins/$plugin_name" $GOPATH/bin/$plugin_name
    cd $DIR
  done
else
  echo "building gatekeeper plugins with release settings..."
  exit 0
fi

exit 0
