package seeddata

import _ "embed"

var (
	//go:embed merged_nist_controls_master_replaced_from_controls_all.json
	controlCatalogJSON []byte

	//go:embed NFR_incremental_keys_with_domain.json
	securityNFRJSON []byte
)

func ControlCatalogJSON() []byte {
	return append([]byte(nil), controlCatalogJSON...)
}

func SecurityNFRJSON() []byte {
	return append([]byte(nil), securityNFRJSON...)
}
