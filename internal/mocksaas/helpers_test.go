package mocksaas_test

import "os"

func makeDir(p string) error             { return os.MkdirAll(p, 0o700) }
func writeFile(p string, b []byte) error { return os.WriteFile(p, b, 0o755) }
