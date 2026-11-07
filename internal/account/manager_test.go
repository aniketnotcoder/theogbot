package account

import (
	"errors"
	"testing"
)

func TestSubmitCodeRequiresCodePrompt(t *testing.T) {
	m:=NewManager(1,"hash")
	a:=&account{state:"awaiting_code",code:make(chan prompt,1),pass:make(chan prompt,1)}
	m.users[42]=a
	if err:=m.SubmitPassword(42,"secret");!errors.Is(err,ErrUnexpectedLoginInput){t.Fatalf("SubmitPassword error = %v, want ErrUnexpectedLoginInput",err)}
	if err:=m.SubmitCode(42,"12345");err!=nil{t.Fatalf("SubmitCode error = %v",err)}
	if got:=<-a.code;got.value!="12345"{t.Fatalf("queued code = %q",got.value)}
}

func TestSubmitPasswordRequiresPasswordPrompt(t *testing.T) {
	m:=NewManager(1,"hash")
	a:=&account{state:"awaiting_password",code:make(chan prompt,1),pass:make(chan prompt,1)}
	m.users[42]=a
	if err:=m.SubmitCode(42,"12345");!errors.Is(err,ErrUnexpectedLoginInput){t.Fatalf("SubmitCode error = %v, want ErrUnexpectedLoginInput",err)}
	if err:=m.SubmitPassword(42,"secret");err!=nil{t.Fatalf("SubmitPassword error = %v",err)}
	if got:=<-a.pass;got.value!="secret"{t.Fatalf("queued password = %q",got.value)}
}
