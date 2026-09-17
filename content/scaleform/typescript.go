package scaleform

import (
	"fmt"
	"sort"
	"strings"
)

type TypeScriptAPI struct {
	Path       string
	Methods    []TypeScriptMethod
	Properties []string
}

type TypeScriptMethod struct {
	Name       string
	Parameters []TypeScriptParameter
	IsVariadic bool
	ReturnType string
}

type TypeScriptParameter struct {
	Name       string
	Type       string
	IsOptional bool
}

func TypeScriptConfig() []byte {
	return []byte(`{
  "compilerOptions": {
    "allowJs": false,
    "lib": ["ES2021"],
    "moduleDetection": "force",
    "noEmit": true,
    "noImplicitAny": true,
    "strict": true,
    "target": "ES2021",
    "types": []
  },
  "include": ["scaleform.d.ts", "ui/scaleform/**/*.ts"]
}
`)
}

func TypeScriptDeclarations(globalNames []string, apis []TypeScriptAPI) []byte {
	methods := make(map[string]string, len(simpleActionNames)+32)
	for _, actionName := range simpleActionNames {
		methods[lowerActionName(actionName)] = "(): void"
	}
	additionalMethods := map[string]string{
		"bytecode":       "(opcode: string, payload: string): void",
		"constantPool":   "(...constants: readonly string[]): void",
		"end":            "(): void",
		"function1":      "(parameters: readonly string[], endLabel: string, originalName: string): void",
		"function2":      "(registerCount: number, flags: number, registers: readonly number[], parameters: readonly string[], endLabel: string, originalName: string): void",
		"getMember":      "(...members: readonly string[]): void",
		"getUrl":         "(url: string, target: string): void",
		"gotoFrame":      "(frame: number): void",
		"gotoLabel":      "(label: string): void",
		"if":             "(label: string): void",
		"jump":           "(label: string): void",
		"pushBool":       "(content: boolean): void",
		"pushConstant":   "(content: string): void",
		"pushConstant8":  "(index: number): void",
		"pushConstant16": "(index: number): void",
		"pushDouble":     "(content: number): void",
		"pushDoubleBits": "(bits: string): void",
		"pushFloat":      "(content: number): void",
		"pushFloatBits":  "(bits: string): void",
		"pushInt":        "(content: number): void",
		"pushNull":       "(): void",
		"pushRegister":   "(register: number): void",
		"pushString":     "(content: string): void",
		"pushUndefined":  "(): void",
		"pushVariable":   "(name: string): void",
		"setTarget":      "(target: string): void",
		"storeRegister":  "(register: number): void",
		"with":           "(endLabel: string): void",
	}
	for method, signature := range additionalMethods {
		methods[method] = signature
	}
	methodNames := make([]string, 0, len(methods))
	for method := range methods {
		methodNames = append(methodNames, method)
	}
	sort.Strings(methodNames)

	var source strings.Builder
	source.WriteString("// darkspin generated Scaleform AVM1 declarations v1\n")
	source.WriteString("type AVM1Value = any;\n\n")
	source.WriteString("interface AVM1Runtime {\n")
	for _, method := range methodNames {
		_, _ = fmt.Fprintf(&source, "  %s%s;\n", method, methods[method])
	}
	source.WriteString("}\n\n")
	source.WriteString("interface MovieClip { _alpha: number; _currentframe: number; _height: number; _name: string; _rotation: number; _visible: boolean; _width: number; _x: number; _xscale: number; _y: number; _yscale: number; gotoAndPlay(frame: string | number): void; gotoAndStop(frame: string | number): void; play(): void; stop(): void; }\n")
	source.WriteString("interface Button extends MovieClip { disabled: boolean; enabled: boolean; }\n")
	source.WriteString("interface TextField extends MovieClip { html: boolean; htmlText: string; text: string; }\n")
	source.WriteString("interface ExternalInterfaceAPI { call(method: string, ...args: readonly AVM1Value[]): AVM1Value; }\n\n")
	source.WriteString("interface ObjectConstructor { registerClass(symbol: string, constructor: AVM1Value): void; }\n")
	apiRoots := make(map[string]bool, len(apis))
	for _, api := range apis {
		parts := strings.Split(api.Path, ".")
		if len(parts) > 0 {
			apiRoots[parts[0]] = true
		}
		_, _ = fmt.Fprintf(&source, "interface %s {\n", typeScriptAPIName(api.Path))
		for _, property := range api.Properties {
			childPath := api.Path + "." + property
			_, _ = fmt.Fprintf(&source, "  %s: %s;\n", property, typeScriptAPIName(childPath))
		}
		for _, method := range api.Methods {
			parameters := make([]string, len(method.Parameters))
			for parameterIndex, parameter := range method.Parameters {
				optional := ""
				if parameter.IsOptional {
					optional = "?"
				}
				prefix := ""
				typeName := parameter.Type
				if method.IsVariadic && parameterIndex == len(method.Parameters)-1 {
					prefix = "..."
					optional = ""
					typeName = "readonly " + typeName + "[]"
				}
				parameters[parameterIndex] = fmt.Sprintf("%s%s%s: %s", prefix, parameter.Name, optional, typeName)
			}
			returnType := method.ReturnType
			if returnType == "" {
				returnType = "AVM1Value"
			}
			_, _ = fmt.Fprintf(&source, "  %s(%s): %s;\n", method.Name, strings.Join(parameters, ", "), returnType)
		}
		source.WriteString("  [member: string]: unknown;\n}\n")
	}
	source.WriteString("declare const avm1: AVM1Runtime;\n")
	source.WriteString("declare const ExternalInterface: ExternalInterfaceAPI;\n")
	source.WriteString("declare const _global: AVM1Value;\n")
	source.WriteString("declare const _root: AVM1Value;\n")
	for _, globalName := range globalNames {
		if globalName == "ExternalInterface" || globalName == "Object" || globalName == "_global" || globalName == "_root" || globalName == "avm1" || apiRoots[globalName] {
			continue
		}
		_, _ = fmt.Fprintf(&source, "declare const %s: AVM1Value;\n", globalName)
	}
	rootNames := make([]string, 0, len(apiRoots))
	for rootName := range apiRoots {
		rootNames = append(rootNames, rootName)
	}
	sort.Strings(rootNames)
	for _, rootName := range rootNames {
		_, _ = fmt.Fprintf(&source, "declare const %s: %s;\n", rootName, typeScriptAPIName(rootName))
	}
	for register := 0; register <= 255; register++ {
		_, _ = fmt.Fprintf(&source, "declare const register%d: AVM1Value;\n", register)
	}
	return []byte(source.String())
}

func typeScriptAPIName(path string) string {
	parts := strings.Split(path, ".")
	var name strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		name.WriteString(strings.ToUpper(part[:1]))
		name.WriteString(part[1:])
	}
	name.WriteString("API")
	return name.String()
}
