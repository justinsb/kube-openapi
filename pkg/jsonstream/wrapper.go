package jsonstream

import (
	"fmt"
	"reflect"
	"strings"

	"k8s.io/kube-openapi/pkg/jsonstream/encoding/json"
)

type messageType interface {
	unmarshalInto(dest reflect.Value, d decoder) error
	// unmarshalNew(d decoder) (reflect.Value, error)
	// FindField(name string) fieldParser
}

// type pointerMessageType struct {
// 	inner messageType
// }

// func (t *pointerMessageType) unmarshalInto(dest reflect.Value, d decoder) error {
// 	elem
// 	return t.inner.unmarshalInto(dest, d)
// }

type structMessageType struct {
	reflectType reflect.Type
	path        string
	fields      map[string]*structField
	// fields []*structField
}

type structField struct {
	name      string
	nameBytes []byte
	field     reflect.StructField
	fieldPath []int
	parser    valueParser
}

// func (t *structMessageType) FindField(name string) *structField {
// 	return t.fields[name]
// }

type inprogressMessageType struct {
	resolved messageType
}

func (t *inprogressMessageType) unmarshalInto(dest reflect.Value, d decoder) error {
	return t.resolved.unmarshalInto(dest, d)
}

type message struct {
	messageType messageType
	val         any
}

func (m *message) unmarshal(d decoder) error {
	dest := reflect.ValueOf(m.val)
	return m.messageType.unmarshalInto(dest, d)
}

type fieldParser interface {
	unmarshal(m message, d decoder) error
}

type valueParser interface {
	unmarshalInto(v reflect.Value, d decoder) error
}

type typeRegistry struct {
	byType map[reflect.Type]messageType
}

var allTypes typeRegistry

func init() {
	allTypes.byType = make(map[reflect.Type]messageType)
}

func (r *typeRegistry) wrapObject(m any) (message, error) {
	t := reflect.TypeOf(m)
	messageType, found := r.byType[t]
	if !found {
		// Handle recursive structures
		inprogress := &inprogressMessageType{}

		r.byType[t] = inprogress

		mt, err := r.buildMessageType(t)
		if err != nil {
			return message{}, err
		}
		inprogress.resolved = mt
		messageType = mt
		r.byType[t] = messageType
	}
	return message{
		messageType: messageType,
		val:         m,
	}, nil
}

func (r *typeRegistry) buildValueParser(t reflect.Type, path string) (valueParser, error) {
	if t.Implements(reflect.TypeOf((*UnmarshalJSONStream)(nil)).Elem()) {
		return r.buildCustomUnmarshaller(t, path)
	}

	if reflect.PointerTo(t).Implements(reflect.TypeOf((*UnmarshalJSONStream)(nil)).Elem()) {
		return r.buildCustomUnmarshallerStruct(t, path)
	}

	switch t.Kind() {
	case reflect.Struct:
		return r.buildStructMessageType(t, path)

	case reflect.Map:
		return r.buildMapParser(t, path)

	case reflect.Slice:
		return r.buildSliceParser(t, path)

	case reflect.String:
		return parserForString, nil

	case reflect.Bool:
		return parserForBool, nil

	case reflect.Float32:
		return parserForFloat32, nil

	case reflect.Float64:
		return parserForFloat64, nil

	case reflect.Int64:
		return parserForInt64, nil
	case reflect.Int32:
		return parserForInt32, nil

	case reflect.Uint64:
		return parserForUint64, nil
	case reflect.Uint32:
		return parserForUint32, nil
	case reflect.Uint16:
		return parserForUint16, nil
	case reflect.Uint8:
		return parserForUint8, nil

	case reflect.Ptr:
		return r.buildPointerParser(t, path)

	case reflect.Interface:
		return r.buildInterfaceParser(t, path)

	default:
		return nil, fmt.Errorf("buildValueParser does not handle kind %v in %q", t.Kind(), path)
	}
}

func (r *typeRegistry) buildMessageType(t reflect.Type) (messageType, error) {
	path := typeName(t)

	if t.Implements(reflect.TypeOf((*UnmarshalJSONStream)(nil)).Elem()) {
		return r.buildCustomUnmarshaller(t, path)
	}

	switch t.Kind() {
	case reflect.Ptr:
		return r.buildPointerParser(t, path)

	case reflect.Struct:
		return r.buildStructMessageType(t, path)

	default:
		return nil, fmt.Errorf("buildMessageType does not handle kind %v", t.Kind())
	}
}

func (r *typeRegistry) buildStructMessageType(t reflect.Type, path string) (messageType, error) {
	ret := &structMessageType{
		reflectType: t,
		path:        path,
		fields:      make(map[string]*structField, t.NumField()),
	}
	var addFields func(t reflect.Type, path string, parentFieldPath []int) error
	addFields = func(t reflect.Type, path string, parentFieldPath []int) error {
		n := t.NumField()
		for i := 0; i < n; i++ {
			field := t.Field(i)

			fieldPath := append([]int{}, parentFieldPath...)
			fieldPath = append(fieldPath, i)

			var jsonName string

			jsonTag := field.Tag.Get("json")
			jsonTagTokens := strings.Split(jsonTag, ",")
			if len(jsonTagTokens) == 0 {
				jsonName = field.Name
			} else if len(jsonTagTokens) == 1 {
				jsonName = jsonTagTokens[0]
			} else if len(jsonTagTokens) == 2 {
				if jsonTagTokens[1] == "omitempty" {
					jsonName = jsonTagTokens[0]
				}
			}
			if jsonName == "-" {
				// Not serialized, ignore field
				continue
			}

			if field.Anonymous {
				if err := addFields(field.Type, path+"."+field.Name, fieldPath); err != nil {
					return err
				}
				continue
			}

			if jsonName == "" {
				return fmt.Errorf("cannot parse json tag %q for field %q in path %v", jsonTag, field.Name, path)
			}

			parser, err := r.buildValueParser(field.Type, path+"."+jsonName)
			if err != nil {
				return err
			}

			ret.fields[jsonName] = &structField{
				name:      jsonName,
				nameBytes: []byte(jsonName),
				field:     field,
				fieldPath: fieldPath,
				parser:    parser,
			}
		}
		return nil
	}

	if err := addFields(t, path, []int{}); err != nil {
		return nil, err
	}

	return ret, nil
}

// type structParser struct {
// 	messageType *structMessageType
// }

func (p *structMessageType) unmarshalInto(v reflect.Value, d decoder) error {
	tok, err := d.Read()
	if err != nil {
		return err
	}
	if tok.Kind() != json.ObjectOpen {
		return d.unexpectedTokenError(tok, p.path, "expected object open")
	}

	for {
		// Read field name.
		tok, err := d.Read()
		if err != nil {
			return err
		}
		var name string
		switch tok.Kind() {
		default:
			return d.unexpectedTokenError(tok, p.path, "expected object close or name")
		case json.ObjectClose:
			return nil
		case json.Name:
			name = tok.Name()
			// case json.String:
			// 	name = tok.String()
		}

		// // Unmarshaling a non-custom embedded message in Any will contain the
		// // JSON field "@type" which should be skipped because it is not a field
		// // of the embedded message, but simply an artifact of the Any format.
		// if skipTypeURL && name == "@type" {
		// 	d.Read()
		// 	continue
		// }

		// Get the FieldDescriptor.
		// var fd *structField
		// for _, v := range p.fields {
		// 	if bytes.Equal(v.nameBytes, name) {
		// 		fd = v
		// 		// klog.Infof("found field %v %v", fd.name, string(name))
		// 	}
		// }

		fd := p.fields[name]

		// var fd protoreflect.FieldDescriptor
		// if strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") {
		// 	// Only extension names are in [name] format.
		// 	extName := protoreflect.FullName(name[1 : len(name)-1])
		// 	extType, err := d.opts.Resolver.FindExtensionByName(extName)
		// 	if err != nil && err != protoregistry.NotFound {
		// 		return d.newError(tok.Pos(), "unable to resolve %s: %v", tok.RawString(), err)
		// 	}
		// 	if extType != nil {
		// 		fd = extType.TypeDescriptor()
		// 		if !messageDesc.ExtensionRanges().Has(fd.Number()) || fd.ContainingMessage().FullName() != messageDesc.FullName() {
		// 			return d.newError(tok.Pos(), "message %v cannot be extended by %v", messageDesc.FullName(), fd.FullName())
		// 		}
		// 	}
		// } else {
		// 	// The name can either be the JSON name or the proto field name.
		// 	fd = fieldDescs.ByJSONName(name)
		// 	if fd == nil {
		// 		fd = fieldDescs.ByTextName(name)
		// 	}
		// }
		// if flags.ProtoLegacy {
		// 	if fd != nil && fd.IsWeak() && fd.Message().IsPlaceholder() {
		// 		fd = nil // reset since the weak reference is not linked in
		// 	}
		// }

		if fd == nil {
			// Field is unknown.
			if d.opts.UnknownFields == nil {
				if err := d.SkipJSONValue(); err != nil {
					return err
				}
				//return d.newError(tok.Pos(), "unknown field %v in %s", tok.RawString(), typeName(p.reflectType))
			} else if err := d.opts.UnknownFields(name, d.Decoder); err != nil {
				return err
			}
			continue
		}

		// Do not allow duplicate fields.
		// num := uint64(fd.Number())
		// if seenNums.Has(num) {
		// 	return d.newError(tok.Pos(), "duplicate field %v", tok.RawString())
		// }
		// seenNums.Set(num)

		// No need to set values for JSON null unless the field type is
		// google.protobuf.Value or google.protobuf.NullValue.
		if tok, _ := d.Peek(); tok.Kind() == json.Null /*&& !isKnownValue(fd) && !isNullValue(fd)*/ {
			d.Read()
			continue
		}

		if err := fd.parser.unmarshalInto(v.FieldByIndex(fd.fieldPath), d); err != nil {
			return err
		}
		// if err := fd.unmarshal(m, d); err != nil {
		// 	return err
		// }
		// switch {
		// case fd.IsList():
		// 	list := m.Mutable(fd).List()
		// 	if err := d.unmarshalList(list, fd); err != nil {
		// 		return err
		// 	}
		// case fd.IsMap():
		// 	mmap := m.Mutable(fd).Map()
		// 	if err := d.unmarshalMap(mmap, fd); err != nil {
		// 		return err
		// 	}
		// default:
		// 	// // If field is a oneof, check if it has already been set.
		// 	// if od := fd.ContainingOneof(); od != nil {
		// 	// 	idx := uint64(od.Index())
		// 	// 	if seenOneofs.Has(idx) {
		// 	// 		return d.newError(tok.Pos(), "error parsing %s, oneof %v is already set", tok.RawString(), od.FullName())
		// 	// 	}
		// 	// 	seenOneofs.Set(idx)
		// 	// }

		// 	// Required or optional fields.
		// 	if err := d.unmarshalSingular(m, fd); err != nil {
		// 		return err
		// 	}
		// }
	}
}

type mapParser struct {
	reflectType reflect.Type
	keyParser   valueParser
	valueParser valueParser
}

func (r *typeRegistry) buildMapParser(t reflect.Type, path string) (*mapParser, error) {
	keyParser, err := r.buildValueParser(t.Key(), path+"[@key]")
	if err != nil {
		return nil, err
	}
	valueParser, err := r.buildValueParser(t.Elem(), path+"->")
	if err != nil {
		return nil, err
	}
	return &mapParser{reflectType: t, keyParser: keyParser, valueParser: valueParser}, nil
}

func (p *mapParser) unmarshalInto(dest reflect.Value, d decoder) error {
	if dest.IsNil() {
		dest.Set(reflect.MakeMap(p.reflectType))
	}

	tok, err := d.Read()
	if err != nil {
		return err
	}
	if tok.Kind() != json.ObjectOpen {
		return d.unexpectedTokenError(tok, "todo", "expected object open")
	}

	// // Determine ahead whether map entry is a scalar type or a message type in
	// // order to call the appropriate unmarshalMapValue func inside the for loop
	// // below.
	// var unmarshalMapValue func() (protoreflect.Value, error)
	// switch fd.MapValue().Kind() {
	// case protoreflect.MessageKind, protoreflect.GroupKind:
	// 	unmarshalMapValue = func() (protoreflect.Value, error) {
	// 		val := mmap.NewValue()
	// 		if err := d.unmarshalMessage(val.Message(), false); err != nil {
	// 			return protoreflect.Value{}, err
	// 		}
	// 		return val, nil
	// 	}
	// default:
	// 	unmarshalMapValue = func() (protoreflect.Value, error) {
	// 		return d.unmarshalScalar(fd.MapValue())
	// 	}
	// }

Loop:
	for {
		// Read field name.
		tok, err := d.Peek()
		if err != nil {
			return err
		}
		switch tok.Kind() {
		default:
			return d.unexpectedTokenError(tok, "todo", "expected object close or name")
		case json.ObjectClose:
			d.Read()
			break Loop
		case json.Name:
			// Continue.
		}

		// Unmarshal field name.
		key := reflect.New(p.reflectType.Key()).Elem()
		if err := p.keyParser.unmarshalInto(key, d); err != nil {
			return err
		}

		// Check for duplicate field name.
		// if dest.MapIndex(key) != zero {
		// 	return d.newError(tok.Pos(), "duplicate map key %v", tok.RawString())
		// }

		// Read and unmarshal field value.
		value := reflect.New(p.reflectType.Elem()).Elem()
		if err := p.valueParser.unmarshalInto(value, d); err != nil {
			return err
		}

		dest.SetMapIndex(key, value)
	}

	return nil
}

type stringParser struct {
}

var parserForString = &stringParser{}

func (p *stringParser) unmarshalInto(dest reflect.Value, d decoder) error {
	tok, err := d.Read()
	if err != nil {
		return err
	}

	// 	kind := fd.Kind()
	// 	switch kind {
	// 	case protoreflect.BoolKind:
	// 		if tok.Kind() == json.Bool {
	// 			return protoreflect.ValueOfBool(tok.Bool()), nil
	// 		}

	// 	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
	// 		if v, ok := unmarshalInt(tok, b32); ok {
	// 			return v, nil
	// 		}

	// 	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
	// 		if v, ok := unmarshalInt(tok, b64); ok {
	// 			return v, nil
	// 		}

	// 	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
	// 		if v, ok := unmarshalUint(tok, b32); ok {
	// 			return v, nil
	// 		}

	// 	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
	// 		if v, ok := unmarshalUint(tok, b64); ok {
	// 			return v, nil
	// 		}

	// 	case protoreflect.DoubleKind:
	// 		if v, ok := unmarshalFloat(tok, b64); ok {
	// 			return v, nil
	// 		}

	// 	case protoreflect.StringKind:
	if tok.Kind() == json.String {
		dest.Set(reflect.ValueOf(tok.ParsedString()))
		return nil
	}

	if tok.Kind() == json.Name {
		dest.Set(reflect.ValueOf(string(tok.Name())))
		return nil
	}

	// 	case protoreflect.BytesKind:
	// 		if v, ok := unmarshalBytes(tok); ok {
	// 			return v, nil
	// 		}

	// 	case protoreflect.EnumKind:
	// 		if v, ok := unmarshalEnum(tok, fd); ok {
	// 			return v, nil
	// 		}

	// 	default:
	// 		panic(fmt.Sprintf("unmarshalScalar: invalid scalar kind %v", kind))
	// 	}

	return d.newError(tok.Pos(), "invalid value for %T: kind=%v raw=%v", p, tok.Kind(), tok.RawString())
}

type boolParser struct {
}

var parserForBool = &boolParser{}

func (p *boolParser) unmarshalInto(dest reflect.Value, d decoder) error {
	tok, err := d.Read()
	if err != nil {
		return err
	}

	if tok.Kind() == json.Bool {
		dest.Set(reflect.ValueOf(tok.Bool()))
	}

	return d.newError(tok.Pos(), "invalid value for %T type: %v", p, tok.RawString())
}

type floatParser struct {
	bits int
}

var parserForFloat64 = &floatParser{bits: 64}
var parserForFloat32 = &floatParser{bits: 32}

func (p *floatParser) unmarshalInto(dest reflect.Value, d decoder) error {
	tok, err := d.Read()
	if err != nil {
		return err
	}

	if v, ok := unmarshalFloat(tok, p.bits); ok {
		dest.Set(v)
	}

	return d.newError(tok.Pos(), "invalid value for %T type: %v", p, tok.RawString())
}

type intParser struct {
	bits int
}

var parserForInt64 = &intParser{bits: 64}
var parserForInt32 = &intParser{bits: 32}

func (p *intParser) unmarshalInto(dest reflect.Value, d decoder) error {
	tok, err := d.Read()
	if err != nil {
		return err
	}

	if v, ok := unmarshalInt(tok, p.bits); ok {
		dest.Set(v)
	}

	return d.newError(tok.Pos(), "invalid value for %T type: %v", p, tok.RawString())
}

type uintParser struct {
	bits int
}

var parserForUint64 = &intParser{bits: 64}
var parserForUint32 = &intParser{bits: 32}
var parserForUint16 = &intParser{bits: 16}
var parserForUint8 = &intParser{bits: 8}

func (p *uintParser) unmarshalInto(dest reflect.Value, d decoder) error {
	tok, err := d.Read()
	if err != nil {
		return err
	}

	if v, ok := unmarshalUint(tok, p.bits); ok {
		dest.Set(v)
	}

	return d.newError(tok.Pos(), "invalid value for %T type: %v", p, tok.RawString())
}

type sliceParser struct {
	reflectType reflect.Type
	elemParser  valueParser
	path        string
}

func (r *typeRegistry) buildSliceParser(t reflect.Type, path string) (*sliceParser, error) {
	elemParser, err := r.buildValueParser(t.Elem(), path+"[]")
	if err != nil {
		return nil, err
	}
	return &sliceParser{reflectType: t, elemParser: elemParser, path: path}, nil
}

func (p *sliceParser) unmarshalInto(dest reflect.Value, d decoder) error {
	tok, err := d.Read()
	if err != nil {
		return err
	}
	if tok.Kind() != json.ArrayOpen {
		return d.unexpectedTokenError(tok, p.path, "expected array open")
	}

	// switch fd.Kind() {
	// case protoreflect.MessageKind, protoreflect.GroupKind:
	// 	for {
	// 		tok, err := d.Peek()
	// 		if err != nil {
	// 			return err
	// 		}

	// 		if tok.Kind() == json.ArrayClose {
	// 			d.Read()
	// 			return nil
	// 		}

	// 		val := list.NewElement()
	// 		if err := d.unmarshalMessage(val.Message(), false); err != nil {
	// 			return err
	// 		}
	// 		list.Append(val)
	// 	}
	// default:
	for {
		tok, err := d.Peek()
		if err != nil {
			return err
		}

		if tok.Kind() == json.ArrayClose {
			d.Read()
			return nil
		}

		val := reflect.New(p.reflectType.Elem()).Elem()
		if err := p.elemParser.unmarshalInto(val, d); err != nil {
			return err
		}
		appended := reflect.Append(dest, val)
		dest.Set(appended)
	}
	// }

	// return nil
}

type UnmarshalJSONStream interface {
	UnmarshalJSONStream(d *json.Decoder) error
}

type customUnmarshaller struct {
	reflectType reflect.Type
}

func (r *typeRegistry) buildCustomUnmarshaller(t reflect.Type, path string) (*customUnmarshaller, error) {
	return &customUnmarshaller{reflectType: t}, nil
}

func (p *customUnmarshaller) unmarshalInto(dest reflect.Value, d decoder) error {
	if dest.IsNil() {
		dest.Set(reflect.New(p.reflectType.Elem()))
	}
	intf := dest.Interface()
	return intf.(UnmarshalJSONStream).UnmarshalJSONStream(d.Decoder)
}

type customUnmarshallerStruct struct {
	reflectType reflect.Type
	path        string
}

func (r *typeRegistry) buildCustomUnmarshallerStruct(t reflect.Type, path string) (*customUnmarshallerStruct, error) {
	return &customUnmarshallerStruct{reflectType: t, path: path}, nil
}

func (p *customUnmarshallerStruct) unmarshalInto(dest reflect.Value, d decoder) error {
	intf := dest.Addr().Interface()
	return intf.(UnmarshalJSONStream).UnmarshalJSONStream(d.Decoder)
}

type pointerParser struct {
	reflectType reflect.Type
	elemParser  valueParser
}

func (r *typeRegistry) buildPointerParser(t reflect.Type, path string) (*pointerParser, error) {
	elemParser, err := r.buildValueParser(t.Elem(), path)
	if err != nil {
		return nil, err
	}
	return &pointerParser{reflectType: t, elemParser: elemParser}, nil
}

func (p *pointerParser) unmarshalInto(dest reflect.Value, d decoder) error {
	if dest.IsNil() {
		dest.Set(reflect.New(p.reflectType.Elem()))
	}
	elem := dest.Elem()
	return p.elemParser.unmarshalInto(elem, d)
}

type interfaceParser struct {
	reflectType reflect.Type
	path        string
}

func (r *typeRegistry) buildInterfaceParser(t reflect.Type, path string) (*interfaceParser, error) {
	return &interfaceParser{reflectType: t, path: path}, nil
}

func (p *interfaceParser) unmarshalInto(dest reflect.Value, d decoder) error {
	tok, err := d.Peek()
	if err != nil {
		return err
	}
	switch tok.Kind() {
	case json.String:
		d.Read()
		dest.Set(reflect.ValueOf(tok.ParsedString()))
		return nil

	case json.ArrayOpen:
		var slice []interface{}
		// TODO: Create ParseSlice helper?
		p, err := allTypes.buildValueParser(reflect.TypeOf(slice), p.path+"[]")
		if err != nil {
			return err
		}
		v := reflect.ValueOf(&slice).Elem()
		if err := p.unmarshalInto(v, d); err != nil {
			return err
		}
		dest.Set(v)
		return nil

	case json.ObjectOpen:
		var obj map[string]interface{}
		// TODO: Create ParseObject helper?
		p, err := allTypes.buildValueParser(reflect.TypeOf(obj), p.path+"{}")
		if err != nil {
			return err
		}
		v := reflect.ValueOf(&obj).Elem()
		if err := p.unmarshalInto(v, d); err != nil {
			return err
		}
		dest.Set(v)
		return nil

	default:
		return d.unexpectedTokenError(tok, p.path, "expected string or arrayOpen or objectOpen")
	}
}

func typeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Ptr:
		return "*" + typeName(t.Elem())
	case reflect.Struct:
		s := t.Name()
		if s != "" {
			return s
		}
		v := reflect.New(t)
		return fmt.Sprintf("%T", v.Elem().Interface())
	default:
		return t.Name()
	}
}
