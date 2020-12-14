package cs

type GolServer struct {
	OnKeyPress func(rune)
}

type KeyPressParam struct {
	Key rune
}
type KeyPressResponse struct{}

func (gs *GolServer) KeyPress(param *KeyPressParam, response *KeyPressResponse) error {
	gs.OnKeyPress(param.Key)
	return nil
}
