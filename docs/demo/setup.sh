#!/usr/bin/env sh
set -eu

REVUI_ROOT="$(pwd)"
DEMO_DIR="${TMPDIR:-/tmp}/revui-demo"
rm -rf "$DEMO_DIR"
mkdir -p "$DEMO_DIR/internal/review" "$DEMO_DIR/cmd/revui" "$DEMO_DIR/docs"
cd "$DEMO_DIR"
git init -q -b main
git config user.email revui@example.test
git config user.name "revui demo"
git config commit.gpgsign false
cat > internal/review/session.go <<'EOF'
package review

type Session struct {
	Branch   string
	Base     string
	Reviewed map[string]string
}

func (s Session) IsReviewed(path, fingerprint string) bool {
	return fingerprint != "" && s.Reviewed[path] == fingerprint
}
EOF
cat > cmd/revui/main.go <<'EOF'
package main

import "fmt"

func main() {
	fmt.Println("review before request")
}
EOF
printf "# revui\n\nReview your PR before it's a PR.\n" > README.md
printf 'Local review state belongs in Git metadata.\n' > docs/design.txt
git add .
git commit -q -m base
git switch -q -c feature/review-workflow
perl -0pi -e 's/type Session struct \{/\/\/ Session tracks local review progress.\ntype Session struct {/; s/\tBase     string/\tBase     string\n\tUpdated  int64/' internal/review/session.go
printf '\nfunc version() string { return "v0.1.0" }\n' >> cmd/revui/main.go
printf '\n- Search an entire repository.\n- Copy code with source locations.\n' >> README.md
printf 'No code leaves the machine.\n' > docs/privacy.txt
export REVUI_ROOT DEMO_DIR
