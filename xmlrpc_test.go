package main

import "testing"

func TestXMLRPCRoundTrip(t *testing.T) {
	body := string(xmlrpcEncode("load.raw_start", []any{"", xmlrpcBase64([]byte("d4:infoe")), "d.directory_base.set='/data/x'"}))
	if body != `<?xml version="1.0"?><methodCall><methodName>load.raw_start</methodName><params><param><value><string></string></value></param><param><value><base64>ZDQ6aW5mb2U=</base64></value></param><param><value><string>d.directory_base.set=&#39;/data/x&#39;</string></value></param></params></methodCall>` {
		t.Fatalf("encodage : %s", body)
	}
	resp := `<?xml version="1.0"?><methodResponse><params><param><value><array><data>
	<value><array><data><value><string>ABC</string></value><value>Nom &amp; co</value><value><i8>42</i8></value><value><string>/data/Nom</string></value><value><i8>1</i8></value><value><i8>1</i8></value></data></array></value>
	</data></array></value></param></params></methodResponse>`
	v, err := xmlrpcDecode([]byte(resp))
	if err != nil {
		t.Fatal(err)
	}
	rows := v.([]any)
	row := rows[0].([]any)
	if row[0] != "ABC" || row[1] != "Nom & co" || row[2].(int64) != 42 || row[3] != "/data/Nom" {
		t.Fatalf("décodage : %#v", row)
	}
	fault := `<?xml version="1.0"?><methodResponse><fault><value><struct><member><name>faultCode</name><value><i4>-506</i4></value></member><member><name>faultString</name><value><string>Method not found</string></value></member></struct></value></fault></methodResponse>`
	if _, err := xmlrpcDecode([]byte(fault)); err == nil || err.Error() != "XML-RPC fault : Method not found" {
		t.Fatalf("fault : %v", err)
	}
}
