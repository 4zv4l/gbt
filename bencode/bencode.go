/*
Bencode module to help parsing .torrent file(s).
*/
package bencode

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
)

func decode(r *bufio.Reader) (any, error) {
	b, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	switch {
	case b == 'i': // Integer (e.g., i42e)
		s, err := r.ReadString('e')
		if err != nil {
			return nil, err
		}
		return strconv.Atoi(s[:len(s)-1]) // strip the 'e'

	case b >= '0' && b <= '9': // String (e.g., 4:spam)
		r.UnreadByte()
		lenStr, err := r.ReadString(':')
		if err != nil {
			return nil, err
		}
		length, _ := strconv.Atoi(lenStr[:len(lenStr)-1])

		buf := make([]byte, length)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf), nil

	case b == 'l': // List (e.g., l4:spami42ee)
		var list []any
		for {
			if p, _ := r.Peek(1); len(p) > 0 && p[0] == 'e' {
				r.ReadByte()
				break
			}
			val, err := decode(r)
			if err != nil {
				return nil, err
			}
			list = append(list, val)
		}
		return list, nil

	case b == 'd': // Dictionary (e.g., d3:cow3:mooe)
		dict := make(map[string]any)
		for {
			if p, _ := r.Peek(1); len(p) > 0 && p[0] == 'e' {
				r.ReadByte()
				break
			}
			key, err := decode(r)
			if err != nil {
				return nil, err
			}

			val, err := decode(r)
			if err != nil {
				return nil, err
			}

			dict[key.(string)] = val
		}
		return dict, nil
	}

	return nil, fmt.Errorf("invalid bencode format at byte: %c", b)
}

func encode(w *bufio.Writer, v any) error {
	switch val := v.(type) {
	case int:
		_, err := fmt.Fprintf(w, "i%de", val)
		return err
	case string:
		_, err := fmt.Fprintf(w, "%d:%s", len(val), val)
		return err
	case []any:
		fmt.Fprint(w, "l")
		for _, item := range val {
			if err := encode(w, item); err != nil {
				return err
			}
		}
		_, err := fmt.Fprint(w, "e")
		return err
	case map[string]any:
		fmt.Fprint(w, "d")
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			if err := encode(w, k); err != nil {
				return err
			}
			if err := encode(w, val[k]); err != nil {
				return err
			}
		}
		_, err := fmt.Fprint(w, "e")
		return err
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
}

/*
Decode bencoded data from `r`, uses a bufio.Reader.
*/
func Decode(r io.Reader) (any, error) {
	br := bufio.NewReader(r)
	return decode(br)
}

/*
Encode `v` using bencode writing to `w`, uses a bufio.Writer.
*/
func Encode(w io.Writer, v any) error {
	bw := bufio.NewWriter(w)
	err := encode(bw, v)
	if err != nil {
		return err
	}
	return bw.Flush()
}
