package main
import (
  "fmt"
  "os"
  "os/exec"
  "strings"
)
func main() {
  cmd := exec.Command("cmd", "/d", "/c", "echo %PATH%")
  base := os.Environ()
  extra := []string{"PATH=C:\\venv_bin_MARKER;" + os.Getenv("PATH")}
  cmd.Env = append(append([]string{}, base...), extra...)
  out, err := cmd.Output()
  s := strings.TrimSpace(string(out))
  fmt.Printf("err=%v\n", err)
  fmt.Printf("starts_with_marker=%v\n", strings.HasPrefix(strings.ToUpper(s), strings.ToUpper("C:\\venv_bin_MARKER")))
  fmt.Printf("contains_marker=%v\n", strings.Contains(s, "venv_bin_MARKER"))
  if len(s) > 60 { fmt.Printf("prefix=%s\n", s[:60]) } else { fmt.Printf("out=%s\n", s) }
}
