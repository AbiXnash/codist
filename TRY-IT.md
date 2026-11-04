# Try Codist

Codist reviews Java and Spring Boot code on your own machine. It watches a git repo, checks each commit, and prepares fixes you approve. Your code never leaves your computer.

This guide uses Orbit Commerce, a demo store with 8 microservices and about 96,000 lines of Java. It takes about 10 minutes, plus the model download if you want AI review.

Tested on macOS. Linux should work the same way. Windows is not tested with these steps.

## 1. Install what you need

- Git
- Go 1.22 or newer
- Node.js
- `make` and a C compiler (on macOS, run `xcode-select --install`)

## 2. Download and build

```bash
git clone -b rebuild https://github.com/AbiXnash/codist.git
git clone https://github.com/AbiXnash/orbit-commerce.git

cd codist
make install
make build
```

## 3. Run it on the demo

```bash
cd ../orbit-commerce
../codist/server/bin/codist
```

Your browser opens at http://localhost:8080. If that port is taken, run `PORT=8090 ../codist/server/bin/codist` instead.

Without AI, Codist still runs its rules. You can look around right away:

- **Review**: commits and the issues found in each
- **Graph**: services, classes and how they connect
- **Repository**: services, endpoints, hotspots and costly methods
- **Branches**: what is happening on other branches

## 4. Test it with a bug

Keep Codist running. In a second terminal, from the `orbit-commerce` folder, run this to add a bad method and commit it:

```bash
F=catalog-service/src/main/java/com/orbit/catalog/TryCodist.java
mkdir -p "$(dirname "$F")"
cat > "$F" <<'EOF'
package com.orbit.catalog;

public class TryCodist {
    public int parse(String text) {
        try {
            return Integer.parseInt(text);
        } catch (Exception e) {
        }
        return 0;
    }
}
EOF
git add "$F"
git commit -m "Add a parser"
```

Within a few seconds the commit appears under **Review**, with an issue on the empty `catch`. Low findings wait behind the **Show notes** button.

To undo it afterwards:

```bash
git reset --hard HEAD~1
```

## 5. Add AI review (optional)

Install [Ollama](https://ollama.com), then run these from the `orbit-commerce` folder:

```bash
../codist/server/bin/codist setup
../codist/server/bin/codist doctor
```

`setup` downloads a model that suits your machine. On 16 GB of memory that is about 9 GB. `doctor` shows what is installed and what is missing.

Restart Codist, and new commits also get a model review. On a 16 GB M2 Mac, expect about 2 to 3 minutes per file.

## What to expect

Codist is still being built, so please tell us what breaks or confuses you.

- It clearly named 51 of 118 real bugs in our last full test, so it will miss things.
- It also raises false alarms, so read its findings as suggestions to check.
- It never changes or commits your code without your approval.
