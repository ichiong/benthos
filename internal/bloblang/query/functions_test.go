package query

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/Jeffail/benthos/v3/lib/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFunctions(t *testing.T) {
	type easyMsg struct {
		content string
		meta    map[string]string
	}

	mustFunc := func(name string, args ...interface{}) Function {
		t.Helper()
		fn, err := InitFunction(name, args...)
		require.NoError(t, err)
		return fn
	}

	mustMethod := func(fn Function, name string, args ...interface{}) Function {
		t.Helper()
		fn, err := InitMethod(name, fn, args...)
		require.NoError(t, err)
		return fn
	}

	tests := map[string]struct {
		input    Function
		output   interface{}
		err      string
		messages []easyMsg
		vars     map[string]interface{}
		index    int
	}{
		"check throw function 1": {
			input: mustFunc("throw", "foo"),
			err:   "foo",
		},
		"check throw function 2": {
			input: mustMethod(
				mustFunc("throw", "foo"),
				"catch", "bar",
			),
			output: "bar",
		},
		"check var function": {
			input: mustMethod(
				mustFunc("var", "foo"),
				"uppercase",
			),
			output: "FOOBAR",
			vars: map[string]interface{}{
				"foo": "foobar",
			},
		},
		"check var function object": {
			input: mustMethod(
				mustMethod(
					mustFunc("var", "foo"),
					"get", "bar",
				),
				"uppercase",
			),
			output: "FOOBAR",
			vars: map[string]interface{}{
				"foo": map[string]interface{}{
					"bar": "foobar",
				},
			},
		},
		"check var function error": {
			input: mustFunc("var", "foo"),
			vars:  map[string]interface{}{},
			err:   `variable 'foo' undefined`,
		},
		"check meta function object": {
			input:  mustFunc("meta", "foo"),
			output: "foobar",
			messages: []easyMsg{
				{content: "", meta: map[string]string{
					"foo": "foobar",
				}},
			},
		},
		"check meta function error": {
			input: mustFunc("meta", "foo"),
			vars:  map[string]interface{}{},
			err:   `metadata value 'foo' not found`,
		},
		"check metadata function object": {
			input:  mustFunc("meta", "foo"),
			output: "foobar",
			messages: []easyMsg{
				{content: "", meta: map[string]string{
					"foo": "foobar",
				}},
			},
		},
		"check source_metadata function object": {
			input:  mustFunc("meta", "foo"),
			output: "foobar",
			messages: []easyMsg{
				{content: "", meta: map[string]string{
					"foo": "foobar",
				}},
			},
		},
	}

	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			msg := message.New(nil)
			for _, m := range test.messages {
				part := message.NewPart([]byte(m.content))
				if m.meta != nil {
					for k, v := range m.meta {
						part.Metadata().Set(k, v)
					}
				}
				msg.Append(part)
			}

			for i := 0; i < 10; i++ {
				res, err := test.input.Exec(FunctionContext{
					Vars:     test.vars,
					Maps:     map[string]Function{},
					Index:    test.index,
					MsgBatch: msg,
					NewMsg:   msg.Get(test.index),
				})
				if len(test.err) > 0 {
					require.EqualError(t, err, test.err)
				} else {
					require.NoError(t, err)
				}
				assert.Equal(t, test.output, res)
			}

			// Ensure nothing changed
			for i, m := range test.messages {
				doc, err := msg.Get(i).JSON()
				if err == nil {
					msg.Get(i).SetJSON(doc)
				}
				assert.Equal(t, m.content, string(msg.Get(i).Get()))
			}
		})
	}
}

func TestFunctionTargets(t *testing.T) {
	function := func(name string, args ...interface{}) Function {
		t.Helper()
		fn, err := InitFunction(name, args...)
		require.NoError(t, err)
		return fn
	}

	tests := []struct {
		input  Function
		output []TargetPath
	}{
		{
			input: function("throw", "foo"),
		},
		{
			input: function("json", "foo.bar.baz"),
			output: []TargetPath{
				NewTargetPath(TargetValue, "foo", "bar", "baz"),
			},
		},
		{
			input: NewFieldFunction("foo.bar.baz"),
			output: []TargetPath{
				NewTargetPath(TargetValue, "foo", "bar", "baz"),
			},
		},
		{
			input: function("meta", "foo"),
			output: []TargetPath{
				NewTargetPath(TargetMetadata, "foo"),
			},
		},
		{
			input: function("var", "foo"),
			output: []TargetPath{
				NewTargetPath(TargetVariable, "foo"),
			},
		},
	}

	for i, test := range tests {
		test := test
		t.Run(fmt.Sprintf("%v", i), func(t *testing.T) {
			t.Parallel()

			_, res := test.input.QueryTargets(TargetsContext{
				Maps: map[string]Function{},
			})
			assert.Equal(t, test.output, res)
		})
	}
}

func TestEnvFunction(t *testing.T) {
	key := "BENTHOS_TEST_BLOBLANG_FUNCTION"
	os.Setenv(key, "foobar")
	t.Cleanup(func() {
		os.Unsetenv(key)
	})

	e, err := InitFunction("env", key)
	require.Nil(t, err)

	res, err := e.Exec(FunctionContext{})
	require.NoError(t, err)
	assert.Equal(t, "foobar", res)
}

func TestRandomInt(t *testing.T) {
	e, err := InitFunction("random_int")
	require.Nil(t, err)

	tallies := map[int64]int64{}

	for i := 0; i < 100; i++ {
		res, err := e.Exec(FunctionContext{})
		require.NoError(t, err)
		require.IsType(t, int64(0), res)
		tallies[res.(int64)]++
	}

	// Can't prove it ain't random, but I can kick up a fuss if something
	// stinks.
	assert.GreaterOrEqual(t, len(tallies), 20)
	for _, v := range tallies {
		assert.LessOrEqual(t, v, int64(10))
	}

	// Create a new random_int function with a different seed
	e, err = InitFunction("random_int", 10)
	require.NoError(t, err)

	secondTallies := map[int64]int64{}

	for i := 0; i < 100; i++ {
		res, err := e.Exec(FunctionContext{}.WithValue(i))
		require.NoError(t, err)
		require.IsType(t, int64(0), res)
		secondTallies[res.(int64)]++
	}

	assert.NotEqual(t, tallies, secondTallies)
	assert.GreaterOrEqual(t, len(secondTallies), 20)
	for _, v := range secondTallies {
		assert.LessOrEqual(t, v, int64(10))
	}
}

func TestRandomIntDynamic(t *testing.T) {
	idFn := NewFieldFunction("")

	e, err := InitFunction("random_int", idFn)
	require.NoError(t, err)

	tallies := map[int64]int64{}

	for i := 0; i < 100; i++ {
		res, err := e.Exec(FunctionContext{}.WithValue(i))
		require.NoError(t, err)
		require.IsType(t, int64(0), res)
		tallies[res.(int64)]++
	}

	// Can't prove it ain't random, but I can kick up a fuss if something
	// stinks.
	assert.GreaterOrEqual(t, len(tallies), 20)
	for _, v := range tallies {
		assert.LessOrEqual(t, v, int64(10))
	}

	// Create a new random_int function and feed the same values in
	e, err = InitFunction("random_int", idFn)
	require.NoError(t, err)

	secondTallies := map[int64]int64{}

	for i := 0; i < 100; i++ {
		res, err := e.Exec(FunctionContext{}.WithValue(i))
		require.NoError(t, err)
		require.IsType(t, int64(0), res)
		secondTallies[res.(int64)]++
	}

	assert.Equal(t, tallies, secondTallies)

	// Create a new random_int function and feed the first value in the same,
	// but following values are different.
	e, err = InitFunction("random_int", idFn)
	require.NoError(t, err)

	thirdTallies := map[int64]int64{}

	for i := 0; i < 100; i++ {
		input := i
		if input > 0 {
			input += 10
		}
		res, err := e.Exec(FunctionContext{}.WithValue(input))
		require.NoError(t, err)
		require.IsType(t, int64(0), res)
		thirdTallies[res.(int64)]++
	}

	assert.Equal(t, tallies, thirdTallies)
}

func TestRandomIntDynamicParallel(t *testing.T) {
	tsFn, err := InitFunction("timestamp_unix_nano")
	require.NoError(t, err)

	e, err := InitFunction("random_int", tsFn)
	require.NoError(t, err)

	startChan := make(chan struct{})
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startChan
			for j := 0; j < 100; j++ {
				res, err := e.Exec(FunctionContext{})
				require.NoError(t, err)
				require.IsType(t, int64(0), res)
			}
		}()
	}

	close(startChan)
	wg.Wait()
}
