package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codist/internal/config"
	"codist/internal/gitx"
)

func TestCombinedFindsWhatBreaksOnlyOnceBranchesMeet(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(p, body string) {
		t.Helper()
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctl := func(routes string) string {
		return "package p;\n@RestController\n@RequestMapping(\"/api/pay\")\npublic class PayController {\n" + routes + "}\n"
	}
	write("pom.xml", "<project/>")
	write("pay-service/src/main/java/p/PayController.java", ctl("  @GetMapping(\"/one\")\n  public String one() { return \"\"; }\n  @GetMapping(\"/two\")\n  public String two() { return \"\"; }\n"))
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-qm", "base")
	base := git("rev-parse", "HEAD")
	// One branch stops serving a route.
	git("checkout", "-qb", "remove")
	write("pay-service/src/main/java/p/PayController.java", ctl("  @GetMapping(\"/one\")\n  public String one() { return \"\"; }\n"))
	git("commit", "-qam", "remove two")
	// The other starts calling it.
	git("checkout", "-q", "-b", "call", base)
	write("order-service/src/main/java/o/PayClient.java", "package o;\n@FeignClient(name = \"pay-service\")\npublic interface PayClient {\n  @GetMapping(\"/api/pay/two\")\n  String two();\n}\n")
	git("add", "-A")
	git("commit", "-qm", "call two")
	git("merge", "-q", "--no-ff", "-m", "merge", "remove")
	repo, err := gitx.Open(config.Config{RepoPath: dir, Branch: "call"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := repo.Commit(git("rev-parse", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	got := (&Service{repo: repo}).combined(c)
	if len(got) != 1 || !strings.HasSuffix(got[0].Path, "PayClient.java") || len(got[0].Findings) != 1 || !strings.Contains(got[0].Findings[0].Title, "Feign route not served") || !strings.Contains(got[0].Findings[0].Detail, "combined") {
		t.Fatalf("%+v", got)
	}
}
