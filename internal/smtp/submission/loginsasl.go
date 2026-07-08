package submission

import "github.com/emersion/go-sasl"

// loginServer implements the obsolete-but-widely-used SASL LOGIN mechanism,
// which go-sasl no longer ships a server for. Flow: prompt "Username:", read
// username, prompt "Password:", read password, then authenticate.
type loginServer struct {
	authenticate func(username, password string) error
	username     string
	state        int
}

func newLoginServer(auth func(username, password string) error) sasl.Server {
	return &loginServer{authenticate: auth}
}

func (l *loginServer) Next(response []byte) (challenge []byte, done bool, err error) {
	switch l.state {
	case 0:
		l.state = 1
		// Some clients send the username as the initial response.
		if len(response) > 0 {
			l.username = string(response)
			l.state = 2
			return []byte("Password:"), false, nil
		}
		return []byte("Username:"), false, nil
	case 1:
		l.username = string(response)
		l.state = 2
		return []byte("Password:"), false, nil
	default:
		if err := l.authenticate(l.username, string(response)); err != nil {
			return nil, false, err
		}
		return nil, true, nil
	}
}
