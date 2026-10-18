package aion2

// readVarint lee un varint sin signo en formato LEB128 (máximo 32 bits) que
// empieza en b[off:]. Devuelve el valor y el número de bytes consumidos;
// bytesRead es -1 cuando el varint es inválido (más de 32 bits) o está
// incompleto (el buffer termina a mitad del varint).
//
// El protocolo de Aion 2 usa varints LEB128 de 7 bits por byte con el bit alto
// como continuador. Es la primitiva base de casi todo el framing y el parsing:
// las longitudes de frame, los ids de entidad y las cifras de daño se leen así.
// Nota: el entero devuelto es int32, por lo que valores que usan el bit 31
// vuelven negativos; el llamador debe comprobarlo (ver tryVarint).
func readVarint(b []byte, off int) (int32, int) {
	var v uint32
	shift, n := 0, 0
	for {
		if off+n >= len(b) {
			return -1, -1
		}
		c := b[off+n]
		n++
		v |= uint32(c&0x7F) << shift
		if c&0x80 == 0 {
			return int32(v), n
		}
		shift += 7
		if shift >= 32 {
			return -1, -1
		}
	}
}

// tryVarint lee un varint en *off y avanza el puntero. Falla (y no lo avanza)
// cuando el varint es inválido o negativo. Es la forma habitual de consumir
// campos del protocolo cuando no se sabe de antemano si están presentes.
func tryVarint(b []byte, off *int) (int, bool) {
	v, n := readVarint(b, *off)
	if n <= 0 {
		return 0, false
	}
	*off += n
	if v < 0 {
		return 0, false
	}
	return int(v), true
}
