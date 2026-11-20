package ytdlp

func lockJar(string) (unlock func(), err error) {
	return func() {}, nil
}
