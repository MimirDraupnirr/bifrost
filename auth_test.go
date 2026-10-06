package main

import "testing"

func TestPasswordHash(t *testing.T) {
	h := hashPassword("correct horse")
	if !checkPassword(h, "correct horse") || checkPassword(h, "wrong") || checkPassword("garbage", "x") {
		t.Fatal("vérification du mot de passe")
	}
	if h == hashPassword("correct horse") {
		t.Fatal("le sel doit changer")
	}
}
