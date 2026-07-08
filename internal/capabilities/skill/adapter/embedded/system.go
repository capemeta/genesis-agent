package embedded

import (
	"embed"
	"io/fs"
)

//go:embed skills/*
var systemSkills embed.FS

func SystemFS() (fs.FS, error) {
	return fs.Sub(systemSkills, "skills")
}
