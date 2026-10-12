package seeddata

import "testing"

func TestEmbeddedAssetsReturnCopies(t *testing.T) {
	controlA := ControlCatalogJSON()
	controlB := ControlCatalogJSON()
	if len(controlA) == 0 || len(controlB) == 0 {
		t.Fatal("expected embedded control catalog bytes")
	}
	controlA[0] = 'X'
	if controlB[0] == 'X' {
		t.Fatal("ControlCatalogJSON should return a copy")
	}

	nfrA := SecurityNFRJSON()
	nfrB := SecurityNFRJSON()
	if len(nfrA) == 0 || len(nfrB) == 0 {
		t.Fatal("expected embedded security NFR bytes")
	}
	nfrA[0] = 'Y'
	if nfrB[0] == 'Y' {
		t.Fatal("SecurityNFRJSON should return a copy")
	}
}
