package json

// skipJSONValue parses a JSON value (null, boolean, string, number, object and
// array) in order to advance the read to the next JSON value. It relies on
// the decoder returning an error if the types are not in valid sequence.
func (d *Decoder) SkipJSONValue() error {
	tok, err := d.Read()
	if err != nil {
		return err
	}
	// Only need to continue reading for objects and arrays.
	switch tok.Kind() {
	case ObjectOpen:
		for {
			tok, err := d.Read()
			if err != nil {
				return err
			}
			switch tok.Kind() {
			case ObjectClose:
				return nil
			case Name:
				// Skip object field value.
				if err := d.SkipJSONValue(); err != nil {
					return err
				}
			}
		}

	case ArrayOpen:
		for {
			tok, err := d.Peek()
			if err != nil {
				return err
			}
			switch tok.Kind() {
			case ArrayClose:
				d.Read()
				return nil
			default:
				// Skip array item.
				if err := d.SkipJSONValue(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
