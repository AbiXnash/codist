#!/bin/sh
# Sets up Codist for development on macOS or Linux: clones every repository side by side, ties the Go modules
# together with go.work, and installs the interface's packages.
#
#   sh scripts/dev-setup.sh [folder]     (default: the folder that holds this checkout, or ./codist-dev)
#
# Needs git and Go 1.26 or later; Node 20 or later only to work on the interface.
set -eu
OWNER="https://github.com/abxengg"
REPOS="codist codist-java codist-git codist-model codist-review codist-behaviour codist-ui codist-eval"

have() { command -v "$1" >/dev/null 2>&1; }
have git || { echo "dev-setup: install git first." >&2; exit 1; }
have go  || { echo "dev-setup: install Go 1.26 or later from https://go.dev/dl first." >&2; exit 1; }

here=$(cd "$(dirname "$0")/.." 2>/dev/null && pwd || true)
if [ $# -gt 0 ]; then dir=$1
elif [ -n "$here" ] && [ -f "$here/go.mod" ] && grep -q 'module github.com/abxengg/codist$' "$here/go.mod"; then dir=$(dirname "$here")
else dir=$PWD/codist-dev
fi
mkdir -p "$dir"
cd "$dir"
echo "Working in $dir"

for r in $REPOS; do
  if [ -d "$r/.git" ]; then echo "  $r: already there"; else git clone -q "$OWNER/$r.git" && echo "  $r: cloned"; fi
done

# go.work lets each module use the others from these folders instead of their published versions.
cd codist
rm -f go.work go.work.sum
go work init . ../codist-java ../codist-git ../codist-model ../codist-review ../codist-behaviour ../codist-ui
echo "Created codist/go.work"

if have npm; then
  (cd ../codist-ui && npm install --no-audit --no-fund >/dev/null) && echo "Installed the interface's packages"
else
  echo "Node is not installed: fine unless you change the interface (codist-ui)."
fi

echo ""
echo "Check it all:  cd $dir/codist && make test && make build"
echo "Run it:        ./bin/codist /path/to/a/java/repo"
echo "Live reload:   make dev REPO=/path/to/a/java/repo   (interface on http://localhost:5173)"
