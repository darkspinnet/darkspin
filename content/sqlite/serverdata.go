package sqlite

import (
	"bytes"
	"compress/zlib"
	"fmt"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const serverDataLuaType = 0x3681d755

var serverDataGroupName = map[uint32]string{
	0x00000000: "animations~",
	0x02e9c426: "definitions~",
	0x02f98b67: "HintTemplates",
	0x0522de06: "hints~",
	0x24f78aa1: "0x24F78AA1",
	0x32b7d77a: "luadebug",
	0x3681d755: "lua",
	0x3b01d7f6: "LabsTuning",
	0x7153bbb1: "Abilities",
	0x982920ff: "LuaTestScripts",
	0xa35bed24: "0xA35BED24",
	0xc130a42a: "behaviors",
	0xc953ece0: "LuaClient",
	0xd6a2b6e8: "PopupTip",
	0xf6dc1ab3: "NounDefinitionTemplates",
	0xfc0ff8f5: "Modifiers",
	0xffd8419f: "herdtypes~",
}

func serverDataIdentity(entry dbpf.Entry) (string, string, string, bool) {
	resourceGroup := serverDataGroupName[entry.Group]
	if resourceGroup == "" {
		resourceGroup = fmt.Sprintf("0x%08X", entry.Group)
	}
	resourceFormat := serverDataFormat(entry.Type)
	resourceName := fmt.Sprintf("0x%08X.%s", entry.Instance, resourceFormat)
	return resourceGroup, resourceName, resourceFormat, entry.Type == serverDataLuaType
}

func serverDataFormat(typeID uint32) string {
	switch typeID {
	case serverDataLuaType:
		return "lua"
	case 0x00b1b104:
		return "prop"
	case 0x024a0e52:
		return "trigger"
	case 0x1e639c34:
		return "xml"
	default:
		return fmt.Sprintf("0x%08x", typeID)
	}
}

func compressContent(contents []byte) ([]byte, error) {
	buffer := bytes.Buffer{}
	w := zlib.NewWriter(&buffer)
	_, err := w.Write(contents)
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("zlibWrite: %w", err)
	}
	err = w.Close()
	if err != nil {
		return nil, fmt.Errorf("zlibClose: %w", err)
	}
	return buffer.Bytes(), nil
}
