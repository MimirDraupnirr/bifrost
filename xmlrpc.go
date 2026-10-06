package main

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// XML-RPC minimal pour rTorrent : string, int (i4/i8), base64, array,
// struct. Aucune bibliothèque Go de la stdlib ne le fait, et 120 lignes
// couvrent tout ce que `d.multicall2` et `load.raw_start` demandent.

type xmlrpcBase64 []byte

func xmlrpcEncode(method string, params []any) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	xml.EscapeText(&b, []byte(method))
	b.WriteString(`</methodName><params>`)
	for _, p := range params {
		b.WriteString("<param>")
		xmlrpcValue(&b, p)
		b.WriteString("</param>")
	}
	b.WriteString("</params></methodCall>")
	return b.Bytes()
}

func xmlrpcValue(b *bytes.Buffer, v any) {
	b.WriteString("<value>")
	switch x := v.(type) {
	case string:
		b.WriteString("<string>")
		xml.EscapeText(b, []byte(x))
		b.WriteString("</string>")
	case int:
		fmt.Fprintf(b, "<i8>%d</i8>", x)
	case int64:
		fmt.Fprintf(b, "<i8>%d</i8>", x)
	case bool:
		if x {
			b.WriteString("<boolean>1</boolean>")
		} else {
			b.WriteString("<boolean>0</boolean>")
		}
	case xmlrpcBase64:
		b.WriteString("<base64>")
		b.WriteString(base64.StdEncoding.EncodeToString(x))
		b.WriteString("</base64>")
	case []any:
		b.WriteString("<array><data>")
		for _, e := range x {
			xmlrpcValue(b, e)
		}
		b.WriteString("</data></array>")
	default:
		b.WriteString("<string>")
		xml.EscapeText(b, []byte(fmt.Sprint(x)))
		b.WriteString("</string>")
	}
	b.WriteString("</value>")
}

// xmlrpcDecode renvoie la première valeur de la réponse, ou l'erreur du fault.
func xmlrpcDecode(data []byte) (any, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var fault bool
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, errors.New("XML-RPC : réponse sans valeur")
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "fault":
			fault = true
		case "value":
			v, err := xmlrpcReadValue(dec)
			if err != nil {
				return nil, err
			}
			if fault {
				if m, ok := v.(map[string]any); ok {
					return nil, fmt.Errorf("XML-RPC fault : %v", m["faultString"])
				}
				return nil, fmt.Errorf("XML-RPC fault : %v", v)
			}
			return v, nil
		}
	}
}

// xmlrpcReadValue lit le contenu d'un <value> déjà ouvert, jusqu'à sa fermeture.
func xmlrpcReadValue(dec *xml.Decoder) (any, error) {
	var text strings.Builder
	var result any
	typed := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.CharData:
			text.Write(t)
		case xml.StartElement:
			typed = true
			switch t.Name.Local {
			case "array":
				list := []any{}
				for {
					tk, err := dec.Token()
					if err != nil {
						return nil, err
					}
					if s, ok := tk.(xml.StartElement); ok && s.Name.Local == "value" {
						v, err := xmlrpcReadValue(dec)
						if err != nil {
							return nil, err
						}
						list = append(list, v)
					}
					if e, ok := tk.(xml.EndElement); ok && e.Name.Local == "array" {
						break
					}
				}
				result = list
			case "struct":
				m := map[string]any{}
				var name string
				for {
					tk, err := dec.Token()
					if err != nil {
						return nil, err
					}
					if s, ok := tk.(xml.StartElement); ok {
						if s.Name.Local == "name" {
							var n string
							_ = dec.DecodeElement(&n, &s)
							name = n
						} else if s.Name.Local == "value" {
							v, err := xmlrpcReadValue(dec)
							if err != nil {
								return nil, err
							}
							m[name] = v
						}
					}
					if e, ok := tk.(xml.EndElement); ok && e.Name.Local == "struct" {
						break
					}
				}
				result = m
			default:
				var s string
				if err := dec.DecodeElement(&s, &t); err != nil {
					return nil, err
				}
				switch t.Name.Local {
				case "i4", "i8", "int":
					n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
					result = n
				case "boolean":
					result = strings.TrimSpace(s) == "1"
				case "base64":
					b, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
					result = b
				default:
					result = s
				}
			}
		case xml.EndElement:
			if t.Name.Local == "value" {
				if !typed {
					return text.String(), nil // <value>texte</value> = string implicite
				}
				return result, nil
			}
		}
	}
}
