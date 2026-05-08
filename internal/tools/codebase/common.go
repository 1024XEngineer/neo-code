package codebase

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	var buf [20]byte
	bp := len(buf)
	for i > 0 {
		bp--
		buf[bp] = byte('0' + i%10)
		i /= 10
	}
	if negative {
		bp--
		buf[bp] = '-'
	}
	return string(buf[bp:])
}

func boolToString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
