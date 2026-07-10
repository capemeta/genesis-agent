package embedded

import (
	"embed"
	"io/fs"
)

//go:embed all:skills
var systemSkills embed.FS

func SystemFS() (fs.FS, error) {
	return fs.Sub(systemSkills, "skills")
}
