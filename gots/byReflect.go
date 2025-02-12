package gots

import (
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
)

// Generates typescript typings (.d.ts) for given types.
//
// Can wrap types in given namespace (if empty will omit namespace).
//
// If pkg is specified it will generate types in packages containing given prefix.
func GenerateTypeDefinition(out io.StringWriter, namespace string, pkg string, typesToGenerate ...any) error {
	generator := definitionGenerator{
		out:       out,
		namespace: namespace,
		pkg:       pkg,
	}
	return generator.Generate(typesToGenerate...)
}

type definitionGenerator struct {
	out       io.StringWriter
	indent    int
	namespace string
	pkg       string
}

func (this *definitionGenerator) Generate(o ...any) error {
	if this.namespace != "" {
		this.outLine("declare namespace " + this.namespace + " {")
		this.doIndent()
	}
	if err := this.writeDefinition(o...); err != nil {
		return err
	}

	if this.namespace != "" {
		this.outLine("}")
		this.doDeIndent()
	}
	return nil
}

func (this *definitionGenerator) outLine(v string) {
	this.out.WriteString(strings.Repeat("  ", this.indent))
	this.out.WriteString(v)
	this.out.WriteString("\n")
}

func (this *definitionGenerator) doIndent() {
	this.indent++
}

func (this *definitionGenerator) doDeIndent() {
	this.indent--
}

func (this *definitionGenerator) outNext(v string) {
	this.out.WriteString(v)
}

func (this *definitionGenerator) outEndLine() {
	this.out.WriteString("\n")
}

func (this *definitionGenerator) shouldWriteType(t reflect.Type, i exTypeInfo) bool {
	if i.IsBasicType {
		return false
	}
	if !strings.HasPrefix(t.PkgPath(), this.pkg) {
		return false
	}
	return true
}

func (this *definitionGenerator) writeDefinition(o ...any) error {
	typesToProcess := []reflect.Type{}
	processedTypes := map[string]exTypeInfo{}
	for _, obj := range o {
		t := reflect.TypeOf(obj)
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		typeInfo := getTypeInfo(t)

		if _, ok := processedTypes[typeInfo.FullBaseTypeName]; ok {
			continue
		}

		processedTypes[typeInfo.FullBaseTypeName] = typeInfo
		useTypes := this.writeType(t, typeInfo)
		this.outEndLine()
		typesToProcess = append(typesToProcess, useTypes...)
	}

	for len(typesToProcess) > 0 {
		t := typesToProcess[0]
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		typeInfo := getTypeInfo(t)

		typesToProcess = typesToProcess[1:]
		if _, ok := processedTypes[typeInfo.FullBaseTypeName]; ok {
			continue
		}

		processedTypes[typeInfo.FullBaseTypeName] = typeInfo
		if !this.shouldWriteType(t, typeInfo) {
			continue
		}
		useTypes := this.writeType(t, typeInfo)
		this.outEndLine()
		typesToProcess = append(typesToProcess, useTypes...)
	}
	return nil
}

type exTypeInfo struct {
	IsGenericType        bool
	IsBasicType          bool
	IsAlias              bool
	IsInterface          bool
	UnderlyingSystemType reflect.Kind
	BaseType             string
	FullBaseTypeName     string
	TypeParam            string
}

var r = regexp.MustCompile(`(([A-Za-z0-9_]*\.)?([A-Za-z0-9_]+))(?:\[((.*\.)?(.*))\])?`)

func getTypeInfo(t reflect.Type) exTypeInfo {
	tname := t.String()
	match := r.FindStringSubmatch(tname)

	if len(match) < 4 || match[4] == "" {
		if getUnderlyingType(t).Kind() == reflect.Interface {
			i := getUnderlyingType(t)
			return exTypeInfo{
				IsGenericType: false,
				IsBasicType:   isBaseType(t) && !isAliasToBaseType(t),
				IsAlias:       isAliasToBaseType(t),
				IsInterface:   true,

				UnderlyingSystemType: i.Kind(),
				BaseType:             i.Name(),
				FullBaseTypeName:     i.String(),
			}
		} else {
			return exTypeInfo{
				IsGenericType: false,
				IsBasicType:   isBaseType(t) && !isAliasToBaseType(t),
				IsAlias:       isAliasToBaseType(t),

				UnderlyingSystemType: t.Kind(),
				BaseType:             match[3],
				FullBaseTypeName:     tname,
			}
		}
	}

	return exTypeInfo{
		IsGenericType:        true,
		UnderlyingSystemType: t.Kind(),
		BaseType:             match[3],
		TypeParam:            match[6],
		FullBaseTypeName:     match[1],
	}
}

func (this *definitionGenerator) writeType(t reflect.Type, tInfo exTypeInfo) []reflect.Type {
	typeName := tInfo.BaseType

	if tInfo.IsGenericType {
		this.outLine(fmt.Sprintf("type %s<T> = {", typeName))
	} else if isBaseType(t) {
		this.outLine(fmt.Sprintf("type %s = %s & { ", typeName, "unknown"))
	} else {
		this.outLine(fmt.Sprintf("type %s = {", typeName))
	}
	this.doIndent()
	result := this.writeMembers(t, tInfo)

	this.doDeIndent()

	this.outNext("}")

	for _, ao := range result.andAlso {
		oTI := getTypeInfo(ao)
		if oTI.IsGenericType {
			this.outNext(" & " + oTI.BaseType + "<" + oTI.TypeParam + ">")
		} else {
			this.outNext(" & " + ao.Name())
		}
	}
	this.outEndLine()
	return result.usedTypes
}

func (this *definitionGenerator) writeMembers(t reflect.Type, tInfo exTypeInfo) struct {
	andAlso   []reflect.Type
	usedTypes []reflect.Type
} {
	usedTypes := []reflect.Type{}
	andAlso := []reflect.Type{}
	{
		if t.Kind() == reflect.Struct {
			for i := 0; i < t.NumField(); i++ {
				dumpMemberType := true
				fieldInfo := t.Field(i)
				if fieldInfo.IsExported() == false {
					continue
				}

				ft := getUnderlyingType(fieldInfo.Type)
				if fieldInfo.Anonymous {
					andAlso = append(andAlso, ft)
				} else {
					if tInfo.IsGenericType {
						this.outLine(fmt.Sprintf("%s: %s", fieldInfo.Name, this.getTypingNameForGeneric(fieldInfo)))
					} else if ft.Kind() == reflect.Struct && ft.Name() == "" {

						if fieldInfo.Type.Kind() == reflect.Pointer {
							this.outLine(fmt.Sprintf("%s: null | {", fieldInfo.Name))
						} else {
							this.outLine(fmt.Sprintf("%s: {", fieldInfo.Name))
						}
						this.doIndent()
						memberInfo := getTypeInfo(ft)
						membersResult := this.writeMembers(ft, memberInfo)
						usedTypes = append(usedTypes, membersResult.usedTypes...)
						this.doDeIndent()
						this.outLine("}")
						dumpMemberType = false
					} else {
						this.outLine(fmt.Sprintf("%s: %s", fieldInfo.Name, this.getTypingName(fieldInfo.Type)))
					}
				}

				if dumpMemberType {
					switch ft.Kind() {
					case reflect.Slice:
						usedTypes = append(usedTypes, getUnderlyingType(ft.Elem()))
					case reflect.Map:
						usedTypes = append(usedTypes, getUnderlyingType(ft.Key()), getUnderlyingType(ft.Elem()))
					default:
						usedTypes = append(usedTypes, getUnderlyingType(ft))
					}
				}

			}
		}

		if t.Kind() == reflect.Interface {
			mr := this.writeMethods(t)
			andAlso = append(andAlso, mr.andAlso...)
			usedTypes = append(usedTypes, mr.usedTypes...)
		} else {
			ptrType := reflect.PointerTo(t)
			mr := this.writeMethods(ptrType)
			andAlso = append(andAlso, mr.andAlso...)
			usedTypes = append(usedTypes, mr.usedTypes...)
		}
	}

	return struct {
		andAlso   []reflect.Type
		usedTypes []reflect.Type
	}{
		andAlso,
		usedTypes,
	}
}

func (this *definitionGenerator) writeMethods(ptrType reflect.Type) struct {
	andAlso   []reflect.Type
	usedTypes []reflect.Type
} {
	usedTypes := []reflect.Type{}
	andAlso := []reflect.Type{}

	isInterface := ptrType.Kind() == reflect.Interface
	for i := 0; i < ptrType.NumMethod(); i++ {
		methodInfo := ptrType.Method(i)

		numParams := methodInfo.Type.NumIn()
		numResults := methodInfo.Type.NumOut()
		if numResults > 1 {
			//TODO configure to panic
			this.outLine(fmt.Sprintf("// multiple results %s", methodInfo.Name))
			continue
		}

		prmsStr := []string{}

		if isInterface {
			for pI := 0; pI < numParams; pI++ {
				prmType := methodInfo.Type.In(pI)
				usedTypes = append(usedTypes, prmType)
				prmsStr = append(prmsStr, fmt.Sprintf("p%d: %s", pI+1, this.getTypingName(prmType)))
				usedTypes = append(usedTypes, prmType)
			}
		} else {
			for pI := 0; pI < numParams; pI++ {
				prmType := methodInfo.Type.In(pI)
				if pI == 0 {

				} else {
					usedTypes = append(usedTypes, prmType)

					prmsStr = append(prmsStr, fmt.Sprintf("p%d: %s", pI, this.getTypingName(prmType)))
					usedTypes = append(usedTypes, prmType)
				}
			}
		}

		if numResults == 0 {
			this.outLine(fmt.Sprintf("%s(%s): void", methodInfo.Name, strings.Join(prmsStr, ", ")))
		} else {
			resultType := methodInfo.Type.Out(0)
			resultStr := []string{}
			switch resultType.Kind() {
			case reflect.Pointer:
				{
					resultStr = append(resultStr, this.getTypingName(resultType))
					usedTypes = append(usedTypes, resultType.Elem())
				}
			case reflect.Struct:
				{
					resultStr = append(resultStr, this.getTypingName(resultType))
					usedTypes = append(usedTypes, resultType)
				}
			case reflect.Slice:
				{
					resultStr = append(resultStr, this.getTypingName(resultType))
					usedTypes = append(usedTypes, resultType.Elem())
				}
			case reflect.Map:
				{
					resultStr = append(resultStr, this.getTypingName(resultType))
				}
			default:
				resultStr = append(resultStr, this.getTypingName(resultType))
				usedTypes = append(usedTypes, resultType)
			}

			if len(resultStr) > 1 {
				panic("not supported")
			}
			this.outLine(fmt.Sprintf("%s(%s): %s", methodInfo.Name, strings.Join(prmsStr, ", "), strings.Join(resultStr, "  ")))
		}
	}
	return struct {
		andAlso   []reflect.Type
		usedTypes []reflect.Type
	}{
		andAlso,
		usedTypes,
	}
}
func (this *definitionGenerator) getTypingName(t reflect.Type) string {
	tInfo := getTypeInfo(getUnderlyingType(t))
	isAlias := tInfo.IsAlias

	typeName := t.Name()
	if !isAlias {
		switch t.Kind() {
		case reflect.String:
			typeName = "string"
		case reflect.Bool:
			typeName = "boolean"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			typeName = "number"
		case reflect.Uintptr, reflect.Complex64, reflect.Complex128:
			typeName = "object"
		case reflect.Pointer:
			typeName = fmt.Sprintf("null | %s", this.getTypingName(t.Elem()))
		case reflect.Slice:
			if t.Elem().Kind() == reflect.Pointer {
				typeName = fmt.Sprintf("(%s)[]", this.getTypingName(t.Elem()))
			} else {
				typeName = this.getTypingName(t.Elem()) + "[]"
			}
		case reflect.Interface:
			if tInfo.FullBaseTypeName == "interface {}" {
				typeName = fmt.Sprintf("any")
			} else {
				typeName = fmt.Sprintf("null | %s", typeName)
			}
		case reflect.Map:
			keyType := this.getTypingName(t.Key())
			elemType := this.getTypingName(t.Elem())
			typeName = fmt.Sprintf("Record<%s, %s>", keyType, elemType)
		case reflect.Struct:
			isSubGeneric := getTypeInfo(t)
			if isSubGeneric.IsGenericType {
				typeName = fmt.Sprintf("%s<%s>", isSubGeneric.BaseType, isSubGeneric.TypeParam)
			} else if !this.shouldWriteType(t, tInfo) {
				typeName = "unknown"
			} else {
				typeName = fmt.Sprintf("%s", t.Name())
			}
		default:
			typeName = t.Name()
		}
	}
	return typeName
}

func (this *definitionGenerator) getTypingNameForGeneric(fieldInfo reflect.StructField) string {
	_, isGenericParam := fieldInfo.Tag.Lookup("waxGeneric")

	if !isGenericParam {
		return this.getTypingName(fieldInfo.Type)
	}
	return getTypeForKind("T", fieldInfo.Type.Kind())
}

func getTypeForKind(t string, kind reflect.Kind) string {
	switch kind {
	case reflect.Pointer:
		return "null | " + t
	case reflect.Slice:
		return "" + t + "[]"
	default:
		return t
	}
}

func getUnderlyingType(t reflect.Type) reflect.Type {
	switch t.Kind() {
	case reflect.Pointer:
		return t.Elem()
	default:
		return t
	}
}

func isAliasToBaseType(t reflect.Type) bool {
	return t.PkgPath() != "" && isBaseType(t)
}

func isBaseType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Bool, reflect.Complex64, reflect.Complex128, reflect.UnsafePointer:
		return true
	}
	return false
}
