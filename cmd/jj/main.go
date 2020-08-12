package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"

	isatty "github.com/mattn/go-isatty"
	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"
)

var (
	version = "0.0.1"
	tag     = "jj - JSON Stream Editor " + version
	usage   = `
usage: jj [-v value] [-purnOD] [-i infile] [-o outfile] keypath

examples: jj keypath                      read value from stdin
      or: jj -i infile keypath            read value from infile
      or: jj -v value keypath             edit value
      or: jj -v value -o outfile keypath  edit value and write to outfile

options:
      -v value             Edit JSON key path value
      -p                   Make json pretty, keypath is optional
      -u                   Make json ugly, keypath is optional
      -r                   Use raw values, otherwise types are auto-detected
      -n                   Do not output color or extra formatting
      -O                   Performance boost for value updates
      -D                   Delete the value at the specified key path
      -l                   Output array values on multiple lines
      -i infile            Use input file instead of stdin
      -o outfile           Use output file instead of stdout
      keypath              JSON key path (like "name.last")

Getting a value

JJ uses a path syntax for finding values.

Get a string:

$ echo '{"name":{"first":"Tom","last":"Smith"}}' | jj name.last
Smith

Get a block of JSON:

$ echo '{"name":{"first":"Tom","last":"Smith"}}' | jj name
{"first":"Tom","last":"Smith"}

Try to get a non-existent key:

$ echo '{"name":{"first":"Tom","last":"Smith"}}' | jj name.middle
null

Get the raw string value:

$ echo '{"name":{"first":"Tom","last":"Smith"}}' | jj -r name.last
"Smith"

Get an array value by index:

$ echo '{"friends":["Tom","Jane","Carol"]}' | jj friends.1
Jane

JSON Lines

There's support for JSON Lines using the .. path prefix. Which when specified, treats the multi-lined document as an array.

For example:

{"name": "Gilbert", "age": 61}
{"name": "Alexa", "age": 34}
{"name": "May", "age": 57}

..#                   >> 4
..1                   >> {"name": "Alexa", "age": 34}
..#.name              >> ["Gilbert","Alexa","May"]
..#[name="May"].age   >> 57

Setting a value

The path syntax for setting values has a couple of tiny differences than for getting values.

The -v value option is auto-detected as a Number, Boolean, Null, or String. You can override the auto-detection and input raw JSON by including the -r option. This is useful for raw JSON blocks such as object, arrays, or premarshalled strings.

Update a value:

$ echo '{"name":{"first":"Tom","last":"Smith"}}' | jj -v Andy name.first
{"name":{"first":"Andy","last":"Smith"}}

Set a new value:

$ echo '{"name":{"first":"Tom","last":"Smith"}}' | jj -v 46 age
{"age":46,"name":{"first":"Tom","last":"Smith"}}

Set a new nested value:

$ echo '{"name":{"first":"Tom","last":"Smith"}}' | jj -v relax task.today
{"task":{"today":"relax"},"name":{"first":"Tom","last":"Smith"}}

Replace an array value by index:

$ echo '{"friends":["Tom","Jane","Carol"]}' | jj -v Andy friends.1
{"friends":["Tom","Andy","Carol"]}

Append an array:

$ echo '{"friends":["Tom","Jane","Carol"]}' | jj -v Andy friends.-1
{"friends":["Tom","Andy","Carol","Andy"]}

Set an array value that's past the bounds:

$ echo '{"friends":["Tom","Jane","Carol"]}' | jj -v Andy friends.5
{"friends":["Tom","Andy","Carol",null,null,"Andy"]}

Set a raw block of JSON:

$ echo '{"name":"Carol"}' | jj -r -v '["Tom","Andy"]' friends
{"friends":["Tom","Andy"],"name":"Carol"}

Start new JSON document:

$ echo '' | jj -v 'Sam' name.first
{"name":{"first":"Sam"}}

Deleting a value

Delete a value:

$ echo '{"age":46,"name":{"first":"Tom","last":"Smith"}}' | jj -D age
{"name":{"first":"Tom","last":"Smith"}}

Delete an array value by index:

$ echo '{"friends":["Andy","Carol"]}' | jj -D friends.0
{"friends":["Carol"]}

Delete last item in array:

$ echo '{"friends":["Andy","Carol"]}' | jj -D friends.-1
{"friends":["Andy"]}

Optimistically update a value

The -O option can be used when the caller expects that a value at the specified keypath already exists.

Using this option can speed up an operation by as much as 6x, but slow down as much as 20% when the value does not exist.

For example:

echo '{"name":{"first":"Tom","last":"Smith"}}' | jj -v Tim -O name.first

The -O tells jj that the name.first likely exists so try a fasttrack operation first.
Pretty printing

The -p flag will make the output json pretty.

$ echo '{"name":{"first":"Tom","last":"Smith"}}' | jj -p name
{
  "first": "Tom",
  "last": "Smith"
}

Also the keypath is optional when the -p flag is specified, allowing for the entire json document to be made pretty.

$ echo '{"name":{"first":"Tom","last":"Smith"}}' | jj -p
{
  "name": {
    "first": "Tom",
    "last": "Smith"
  }
}

Ugly printing

The -u flag will compress the json into the fewest characters possible by squashing newlines and spaces.
`
)

type args struct {
	infile    *string
	outfile   *string
	value     *string
	raw       bool
	del       bool
	opt       bool
	keypathok bool
	keypath   string
	pretty    bool
	ugly      bool
	notty     bool
	lines     bool
}

func parseArgs() args {
	fail := func(format string, args ...interface{}) {
		fmt.Fprintf(os.Stderr, "%s\n", tag)
		if format != "" {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
		fmt.Fprintf(os.Stderr, "%s\n", usage)
		os.Exit(1)
	}
	help := func() {
		buf := &bytes.Buffer{}
		fmt.Fprintf(buf, "%s\n", tag)
		fmt.Fprintf(buf, "%s\n", usage)
		os.Stdout.Write(buf.Bytes())
		os.Exit(0)
	}
	var a args
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		default:
			if len(os.Args[i]) > 1 && os.Args[i][0] == '-' {
				for j := 1; j < len(os.Args[i]); j++ {
					switch os.Args[i][j] {
					default:
						fail("unknown option argument: \"-%c\"", os.Args[i][j])
					case '-':
						fail("unknown option argument: \"%s\"", os.Args[i])
					case 'p':
						a.pretty = true
					case 'u':
						a.ugly = true
					case 'r':
						a.raw = true
					case 'O':
						a.opt = true
					case 'D':
						a.del = true
					case 'n':
						a.notty = true
					case 'l':
						a.lines = true
					}
				}
				continue
			}
			if !a.keypathok {
				a.keypathok = true
				a.keypath = os.Args[i]
			} else {
				fail("unknown option argument: \"%s\"", a.keypath)
			}
		case "-v", "-i", "-o":
			arg := os.Args[i]
			i++
			if i >= len(os.Args) {
				fail("argument missing after: \"%s\"", arg)
			}
			switch arg {
			case "-v":
				a.value = &os.Args[i]
			case "-i":
				a.infile = &os.Args[i]
			case "-o":
				a.outfile = &os.Args[i]
			}
		case "--force-notty":
			a.notty = true
		case "--version":
			fmt.Fprintf(os.Stdout, "%s\n", tag)
			os.Exit(0)
		case "-h", "--help", "-?":
			help()
		}
	}
	if !a.keypathok && !a.pretty && !a.ugly {
		fail("missing required option: \"keypath\"")
	}
	return a
}

func main() {
	a := parseArgs()
	var input []byte
	var err error
	var outb []byte
	var outs string
	var outa bool
	var outt gjson.Type
	var f *os.File
	if a.infile == nil {
		input, err = ioutil.ReadAll(os.Stdin)
	} else {
		input, err = ioutil.ReadFile(*a.infile)
	}
	if err != nil {
		goto fail
	}
	if a.del {
		outb, err = sjson.DeleteBytes(input, a.keypath)
		if err != nil {
			goto fail
		}
	} else if a.value != nil {
		raw := a.raw
		val := *a.value
		if !raw {
			switch val {
			default:
				if len(val) > 0 {
					if (val[0] >= '0' && val[0] <= '9') || val[0] == '-' {
						if _, err := strconv.ParseFloat(val, 64); err == nil {
							raw = true
						}
					}
				}
			case "true", "false", "null":
				raw = true
			}
		}
		opts := &sjson.Options{}
		if a.opt {
			opts.Optimistic = true
			opts.ReplaceInPlace = true
		}
		if raw {
			// set as raw block
			outb, err = sjson.SetRawBytesOptions(
				input, a.keypath, []byte(val), opts)
		} else {
			// set as a string
			outb, err = sjson.SetBytesOptions(input, a.keypath, val, opts)
		}
		if err != nil {
			goto fail
		}
	} else {
		if !a.keypathok {
			outb = input
		} else {
			res := gjson.GetBytes(input, a.keypath)
			if a.raw {
				outs = res.Raw
			} else {
				outt = res.Type
				outa = res.IsArray()
				outs = res.String()
			}
		}
	}
	if a.outfile == nil {
		f = os.Stdout
	} else {
		f, err = os.Create(*a.outfile)
		if err != nil {
			goto fail
		}
	}
	if outb == nil {
		outb = []byte(outs)
	}
	if a.lines && outa {
		var outb2 []byte
		gjson.ParseBytes(outb).ForEach(func(_, v gjson.Result) bool {
			outb2 = append(outb2, pretty.Ugly([]byte(v.Raw))...)
			outb2 = append(outb2, '\n')
			return true
		})
		outb = outb2
	} else if a.raw || outt != gjson.String {
		if a.pretty {
			outb = pretty.Pretty(outb)
		} else if a.ugly {
			outb = pretty.Ugly(outb)
		}
	}
	if !a.notty && isatty.IsTerminal(f.Fd()) {
		if a.raw || outt != gjson.String {
			outb = pretty.Color(outb, pretty.TerminalStyle)
		} else {
			outb = append([]byte(pretty.TerminalStyle.String[0]), outb...)
			outb = append(outb, pretty.TerminalStyle.String[1]...)
		}
		for len(outb) > 0 && outb[len(outb)-1] == '\n' {
			outb = outb[:len(outb)-1]
		}
		outb = append(outb, '\n')
	}
	f.Write(outb)
	f.Close()
	return
fail:
	fmt.Fprintf(os.Stderr, "error: %v\n", err.Error())
	os.Exit(1)
}
