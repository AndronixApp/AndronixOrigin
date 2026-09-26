// Probe: which Go stdlib paths survive Android's app seccomp filter.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
)

func main() {
	switch os.Args[1] {
	case "lookpath":
		p, err := exec.LookPath("sh")
		fmt.Println(runtime.GOOS, "lookpath:", p, err)
	case "exec": // absolute path: no LookPath, but os.StartProcess (pidfd probe)
		out, err := exec.Command("/system/bin/echo", "hi").CombinedOutput()
		fmt.Printf("%s exec: %q %v\n", runtime.GOOS, out, err)
	case "copy": // io.Copy file->file: copy_file_range
		src, _ := os.Open(os.Args[0])
		dst, _ := os.Create(os.Getenv("HOME") + "/probe.copy")
		n, err := io.Copy(dst, src)
		fmt.Println(runtime.GOOS, "copy:", n, err)
	}
}
