package service

import "testing"

func TestClassifySkillQACommand(t *testing.T) {
	t.Parallel()
	if ClassifySkillQACommand("python thumbnail.py") != ValidatorRenderProof {
		t.Fatal("thumbnail")
	}
	if ClassifySkillQACommand("markitdown deck.pptx") != ValidatorContentQA {
		t.Fatal("markitdown")
	}
	if ClassifySkillQACommand("pdftoppm -png out.pdf") != ValidatorRenderProof {
		t.Fatal("pdftoppm")
	}
}
