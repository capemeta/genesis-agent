package binarygate

import (
	"fmt"
	"path/filepath"
	"strings"

	fscontract "genesis-agent/internal/capabilities/filesystem/contract"
)

// RejectFakeOfficeBinary 阻止用纯文本冒充 OOXML/PDF 交付物。
func RejectFakeOfficeBinary(path string, content []byte) error {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".pptx", ".docx", ".xlsx":
		if len(content) < 4 || content[0] != 'P' || content[1] != 'K' {
			return fscontract.NewError(fscontract.ErrCodeInvalidInput, path, fmt.Errorf("禁止用纯文本冒充 %s；请通过 run_skill_script 生成合法 OOXML", ext))
		}
	case ".pdf":
		if len(content) < 5 || string(content[:5]) != "%PDF-" {
			return fscontract.NewError(fscontract.ErrCodeInvalidInput, path, fmt.Errorf("禁止用纯文本冒充 .pdf；请通过 run_skill_script 或转换脚本生成"))
		}
	}
	return nil
}
