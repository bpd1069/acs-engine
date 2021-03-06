package validator

import (
	"fmt"
	"reflect"
	"strconv"
)

// per validate contruct
type validate struct {
	v              *Validate
	top            reflect.Value
	ns             []byte
	actualNs       []byte
	errs           ValidationErrors
	isPartial      bool
	hasExcludes    bool
	includeExclude map[string]struct{} // reset only if StructPartial or StructExcept are called, no need otherwise

	ffn FilterFunc

	// StructLevel & FieldLevel fields
	slflParent   reflect.Value
	slCurrent    reflect.Value
	flField      reflect.Value
	fldIsPointer bool
	cf           *cField
	ct           *cTag

	// misc reusable values
	misc []byte
	str1 string
	str2 string
}

// parent and current will be the same the first run of validateStruct
func (v *validate) validateStruct(parent reflect.Value, current reflect.Value, typ reflect.Type, ns []byte, structNs []byte, ct *cTag) {

	cs, ok := v.v.structCache.Get(typ)
	if !ok {
		cs = v.v.extractStructCache(current, typ.Name())
	}

	if len(ns) == 0 && len(cs.name) != 0 {

		ns = append(ns, cs.name...)
		ns = append(ns, '.')

		structNs = append(structNs, cs.name...)
		structNs = append(structNs, '.')
	}

	// ct is nil on top level struct, and structs as fields that have no tag info
	// so if nil or if not nil and the structonly tag isn't present
	if ct == nil || ct.typeof != typeStructOnly {

		var f *cField

		for i := 0; i < len(cs.fields); i++ {

			f = cs.fields[i]

			if v.isPartial {

				if v.ffn != nil {
					// used with StructFiltered
					if v.ffn(append(structNs, f.name...)) {
						continue
					}

				} else {
					// used with StructPartial & StructExcept
					_, ok = v.includeExclude[string(append(structNs, f.name...))]

					if (ok && v.hasExcludes) || (!ok && !v.hasExcludes) {
						continue
					}
				}
			}

			v.traverseField(parent, current.Field(f.idx), ns, structNs, f, f.cTags)
		}
	}

	// check if any struct level validations, after all field validations already checked.
	// first iteration will have no info about nostructlevel tag, and is checked prior to
	// calling the next iteration of validateStruct called from traverseField.
	if cs.fn != nil {

		v.slflParent = parent
		v.slCurrent = current
		v.ns = ns
		v.actualNs = structNs

		cs.fn(v)
	}
}

// traverseField validates any field, be it a struct or single field, ensures it's validity and passes it along to be validated via it's tag options
func (v *validate) traverseField(parent reflect.Value, current reflect.Value, ns []byte, structNs []byte, cf *cField, ct *cTag) {

	var typ reflect.Type
	var kind reflect.Kind

	current, kind, v.fldIsPointer = v.extractTypeInternal(current, false)

	switch kind {
	case reflect.Ptr, reflect.Interface, reflect.Invalid:

		if ct == nil {
			return
		}

		if ct.typeof == typeOmitEmpty {
			return
		}

		if ct.hasTag {

			v.str1 = string(append(ns, cf.altName...))

			if v.v.hasTagNameFunc {
				v.str2 = string(append(structNs, cf.name...))
			} else {
				v.str2 = v.str1
			}

			if kind == reflect.Invalid {

				v.errs = append(v.errs,
					&fieldError{
						v:              v.v,
						tag:            ct.aliasTag,
						actualTag:      ct.tag,
						ns:             v.str1,
						structNs:       v.str2,
						fieldLen:       uint8(len(cf.altName)),
						structfieldLen: uint8(len(cf.name)),
						param:          ct.param,
						kind:           kind,
					},
				)

				return
			}

			v.errs = append(v.errs,
				&fieldError{
					v:              v.v,
					tag:            ct.aliasTag,
					actualTag:      ct.tag,
					ns:             v.str1,
					structNs:       v.str2,
					fieldLen:       uint8(len(cf.altName)),
					structfieldLen: uint8(len(cf.name)),
					value:          current.Interface(),
					param:          ct.param,
					kind:           kind,
					typ:            current.Type(),
				},
			)

			return
		}

	case reflect.Struct:

		typ = current.Type()

		if typ != timeType {

			if ct != nil {

				if ct.typeof == typeStructOnly {
					goto CONTINUE
				}

				ct = ct.next
			}

			if ct != nil && ct.typeof == typeNoStructLevel {
				return
			}

		CONTINUE:
			// if len == 0 then validating using 'Var' or 'VarWithValue'
			// Var - doesn't make much sense to do it that way, should call 'Struct', but no harm...
			// VarWithField - this allows for validating against each field withing the struct against a specific value
			//                pretty handly in certain situations
			if len(cf.name) > 0 {
				ns = append(append(ns, cf.altName...), '.')
				structNs = append(append(structNs, cf.name...), '.')
			}

			v.validateStruct(current, current, typ, ns, structNs, ct)
			return
		}
	}

	if !ct.hasTag {
		return
	}

	typ = current.Type()

OUTER:
	for {
		if ct == nil {
			return
		}

		switch ct.typeof {

		case typeOmitEmpty:

			// set Field Level fields
			v.slflParent = parent
			v.flField = current
			v.cf = cf
			v.ct = ct

			if !v.fldIsPointer && !hasValue(v) {
				return
			}

			ct = ct.next
			continue

		case typeDive:

			ct = ct.next

			// traverse slice or map here
			// or panic ;)
			switch kind {
			case reflect.Slice, reflect.Array:

				var i64 int64
				reusableCF := &cField{}

				for i := 0; i < current.Len(); i++ {

					i64 = int64(i)

					v.misc = append(v.misc[0:0], cf.name...)
					v.misc = append(v.misc, '[')
					v.misc = strconv.AppendInt(v.misc, i64, 10)
					v.misc = append(v.misc, ']')

					reusableCF.name = string(v.misc)

					if cf.namesEqual {
						reusableCF.altName = reusableCF.name
					} else {

						v.misc = append(v.misc[0:0], cf.altName...)
						v.misc = append(v.misc, '[')
						v.misc = strconv.AppendInt(v.misc, i64, 10)
						v.misc = append(v.misc, ']')

						reusableCF.altName = string(v.misc)
					}

					v.traverseField(parent, current.Index(i), ns, structNs, reusableCF, ct)
				}

			case reflect.Map:

				var pv string
				reusableCF := &cField{}

				for _, key := range current.MapKeys() {

					pv = fmt.Sprintf("%v", key.Interface())

					v.misc = append(v.misc[0:0], cf.name...)
					v.misc = append(v.misc, '[')
					v.misc = append(v.misc, pv...)
					v.misc = append(v.misc, ']')

					reusableCF.name = string(v.misc)

					if cf.namesEqual {
						reusableCF.altName = reusableCF.name
					} else {
						v.misc = append(v.misc[0:0], cf.altName...)
						v.misc = append(v.misc, '[')
						v.misc = append(v.misc, pv...)
						v.misc = append(v.misc, ']')

						reusableCF.altName = string(v.misc)
					}

					v.traverseField(parent, current.MapIndex(key), ns, structNs, reusableCF, ct)
				}

			default:
				// throw error, if not a slice or map then should not have gotten here
				// bad dive tag
				panic("dive error! can't dive on a non slice or map")
			}

			return

		case typeOr:

			v.misc = v.misc[0:0]

			for {

				// set Field Level fields
				v.slflParent = parent
				v.flField = current
				v.cf = cf
				v.ct = ct

				if ct.fn(v) {

					// drain rest of the 'or' values, then continue or leave
					for {

						ct = ct.next

						if ct == nil {
							return
						}

						if ct.typeof != typeOr {
							continue OUTER
						}
					}
				}

				v.misc = append(v.misc, '|')
				v.misc = append(v.misc, ct.tag...)

				if len(ct.param) > 0 {
					v.misc = append(v.misc, '=')
					v.misc = append(v.misc, ct.param...)
				}

				if ct.next == nil || ct.next.typeof != typeOr { // ct.typeof != typeOr
					// if we get here, no valid 'or' value and no more tags

					v.str1 = string(append(ns, cf.altName...))

					if v.v.hasTagNameFunc {
						v.str2 = string(append(structNs, cf.name...))
					} else {
						v.str2 = v.str1
					}

					if ct.hasAlias {

						v.errs = append(v.errs,
							&fieldError{
								v:              v.v,
								tag:            ct.aliasTag,
								actualTag:      ct.actualAliasTag,
								ns:             v.str1,
								structNs:       v.str2,
								fieldLen:       uint8(len(cf.altName)),
								structfieldLen: uint8(len(cf.name)),
								value:          current.Interface(),
								param:          ct.param,
								kind:           kind,
								typ:            typ,
							},
						)

					} else {

						tVal := string(v.misc)[1:]

						v.errs = append(v.errs,
							&fieldError{
								v:              v.v,
								tag:            tVal,
								actualTag:      tVal,
								ns:             v.str1,
								structNs:       v.str2,
								fieldLen:       uint8(len(cf.altName)),
								structfieldLen: uint8(len(cf.name)),
								value:          current.Interface(),
								param:          ct.param,
								kind:           kind,
								typ:            typ,
							},
						)
					}

					return
				}

				ct = ct.next
			}

		default:

			// set Field Level fields
			v.slflParent = parent
			v.flField = current
			v.cf = cf
			v.ct = ct

			// // report error interface functions need these
			// v.ns = ns
			// v.actualNs = structNs

			if !ct.fn(v) {

				v.str1 = string(append(ns, cf.altName...))

				if v.v.hasTagNameFunc {
					v.str2 = string(append(structNs, cf.name...))
				} else {
					v.str2 = v.str1
				}

				v.errs = append(v.errs,
					&fieldError{
						v:              v.v,
						tag:            ct.aliasTag,
						actualTag:      ct.tag,
						ns:             v.str1,
						structNs:       v.str2,
						fieldLen:       uint8(len(cf.altName)),
						structfieldLen: uint8(len(cf.name)),
						value:          current.Interface(),
						param:          ct.param,
						kind:           kind,
						typ:            typ,
					},
				)

				return

			}

			ct = ct.next
		}
	}

}
