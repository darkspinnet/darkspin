package prop

// propertyNamesByID is the authoritative set of property symbols whose
// encoded IDs have been verified against package data, the client, or the
// SporeModder-FX property registry. DSE never carries the redundant numeric ID.
var propertyNamesByID = map[uint32]string{
	0x00000005: "renderModels",
	0x00B2CCCA: "description",
	0x01CBFDC1: "channel2delay",
	0x03593710: "maxdistance",
	0x04363000: "ShowNetworkBlinky",
	0x04598A6F: "otdbRefillThreshold",
	0x04598A70: "otdbRefillAmount",
	0x046D0560: "soundId",
	0x046D0572: "soundPriority",
	0x04DB21B5: "footcurvelentogain",
	0x050B622E: "RemoveIfOlderThanSeconds",
	0x052F1C91: "classifierList",
	0x052F2304: "classifierTypes",
	0x052F2455: "classifierWeights",
	0x0534676F: "AMTuningVersion",
	0x05433C88: "activeXactMin",
	0x05433CA3: "activeXactMax",
	0x05C29D33: "pollinatorHTTPTimeout",
	0x0609D3A2: "EventTempFileMaxSize",
	0x0609D3D5: "EventUploadPeriodSeconds",
	0x07F28ACC: "polyphonymode",
	0x090A770D: "threadpriority",
	0x09D0B38E: "maxdistancecurve",
	0x0A15B885: "footcurvemasstopitch",
	0x0B65639D: "defaultIn",
	0x0DEB318A: "reverbtime",
	0x0E40C402: "mouthtype",
	0x0F616B72: "mindistance",
	0x0F70317C: "atmospheric",
	0x112590ED: "syms",
	0x12D2A4D4: "is3d",
	0x1446E317: "duckcurve",
	0x14043549: "removegroups",
	0x156573C3: "defaultCr",
	0x173C2710: "wetlevelcurve",
	0x1B8B6C17: "chr1mix",
	0x1E4DB1EB: "attenuation",
	0x1F6EF3D1: "dacoutputmode",
	0x2406A047: "rolloff",
	0x25DF0108: "gain",
	0x29E8B9F8: "startdelay",
	0x2DC2A89D: "weapontype",
	0x2F8B3BF4: "name",
	0x2FE09C83: "send",
	0x37681EA0: "channel1delay",
	0x393F7F7D: "priority",
	0x39B853A3: "footcurvenumlegstogain",
	0x3C2B4EA4: "streambuffersize",
	0x3DA0A727: "primitives",
	0x420C1A8A: "verdanthboss",
	0x45C1CDFE: "cryosboss",
	0x49EB68DB: "wetlevel",
	0x4A253755: "pausegroups",
	0x4A77693A: "pan",
	0x4BC09757: "autoduck",
	0x4B2A3424: "symbols",
	0x51997DB2: "soundtype",
	0x544E850B: "voicetemplate",
	0x56E7C242: "randompitch",
	0x5A32A0CE: "isaggregate",
	0x5ABA5028: "pausegain",
	0x5F174A11: "delaywetdrymix",
	0x5F6317D5: "parent",
	0x61E3EE74: "footcurvemasstogain",
	0x62D9CAD8: "streampoolnumstreams",
	0x64D7BE3C: "listeneroffset",
	0x6C4692B7: "startsounds",
	0x6DD08218: "islooped",
	0x6EB7A717: "pandistance",
	0x6F166215: "dspchain",
	0x6F5B6DA8: "footcurvefootsizetohipass",
	0x701ED91E: "samples",
	0x71BC3009: "pitch",
	0x72CAA95F: "showmindistance",
	0x771119F4: "ignorecontext",
	0x77C00609: "randomstartdelay",
	0x79DA6D74: "streampoolguid",
	0x7CB81A35: "groups",
	0x7F30F249: "footcurveveltogain",
	0x82BEAFE0: "timeinvariantpitch",
	0x86835D38: "enable",
	0x8FC9308E: "probability",
	0x917EE1DB: "footcurvemasstohipass",
	0x9718884E: "BinaryResourceCacheSizeMB",
	0x98AC8996: "fadein",
	0x9A2DDD0F: "shooter",
	0x9BE5D9EE: "channel1feedback",
	0x9E3669DA: "time",
	0xA1D9E5F7: "footcurvefootsizetogain",
	0xB03A6504: "streambufferreadsize",
	0xB59DA191: "gaincurve",
	0xB73EE5C5: "panlfe",
	0xBA03D0CD: "codec",
	0xBA134192: "isvirtual",
	0xBAF7F4F9: "polyphony",
	0xBAFC5CA1: "Editors",
	0xC151440E: "pitchcurve",
	0xC7C535AA: "shadowboss",
	0xD9534BB0: "footcurvefootsizetopitch",
	0xD7E85196: "footcurvelentohipass",
	0xD9A30282: "chr2mix",
	0xDC98300E: "foot_step",
	0xDD94FB65: "foottype",
	0xE7C9EDFB: "sloth",
	0xE8AB99B9: "fadeout",
	0xE91C6F7E: "maxfootsteps",
	0xED1F36D5: "pickup",
	0xED4C265F: "numchannels",
	0xF32E9C29: "channel2feedback",
	0xF58CA0BE: "addgroups",
	0xF5447AE1: "ducks",
	0xF768779C: "citadelboss",
	0xFE04BD3E: "reverbspacesize",
}

var propertyIDsByName = buildPropertyIDsByName()

func buildPropertyIDsByName() map[string]uint32 {
	idsByName := make(map[string]uint32, len(propertyNamesByID))
	for propertyID, propertyName := range propertyNamesByID {
		if _, exists := idsByName[propertyName]; exists {
			panic("duplicate property name: " + propertyName)
		}
		idsByName[propertyName] = propertyID
	}
	return idsByName
}

// Name resolves a verified encoded property ID to its authored symbol.
func Name(propertyID uint32) (string, bool) {
	propertyName, isFound := propertyNamesByID[propertyID]
	return propertyName, isFound
}

// ID resolves an authored property symbol to its verified encoded ID.
func ID(propertyName string) (uint32, bool) {
	propertyID, isFound := propertyIDsByName[propertyName]
	return propertyID, isFound
}
