package gitx

import (
	"reflect"
	"testing"
)

const sample = `diff --git a/A.java b/A.java
index 111..222 100644
--- a/A.java
+++ b/A.java
@@ -1,5 +1,6 @@
 line1
-old2
+new2
+new3
 line4
-gone5
 line6
diff --git a/B.java b/B.java
new file mode 100644
index 000..333
--- /dev/null
+++ b/B.java
@@ -0,0 +1,2 @@
+b1
+b2
diff --git a/C.java b/C.java
deleted file mode 100644
--- a/C.java
+++ /dev/null
@@ -1 +0,0 @@
-c1
diff --git a/old/D.java b/new/D.java
similarity index 100%
rename from old/D.java
rename to new/D.java
`

func TestParseDiff(t *testing.T) {
	files := parseDiff(sample)
	if len(files) != 4 {
		t.Fatalf("got %d files", len(files))
	}
	a := files[0]
	if a.ChangeType != "MODIFY" || a.Path != "A.java" || !reflect.DeepEqual(a.AddedLines, []int{2, 3}) || !reflect.DeepEqual(a.DeletedAt, []int{5}) {
		t.Errorf("A: %+v", a)
	}
	if b := files[1]; b.ChangeType != "ADD" || b.OldPath != "" || !reflect.DeepEqual(b.AddedLines, []int{1, 2}) {
		t.Errorf("B: %+v", b)
	}
	if c := files[2]; c.ChangeType != "DELETE" || c.NewPath != "" || c.Path != "C.java" {
		t.Errorf("C: %+v", c)
	}
	if d := files[3]; d.ChangeType != "RENAME" || d.OldPath != "old/D.java" || d.NewPath != "new/D.java" {
		t.Errorf("D: %+v", d)
	}
}
