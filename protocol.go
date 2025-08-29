//go:build js && wasm

package wasmsqlite

import (
	"database/sql/driver"
	"fmt"
	"syscall/js"
	"time"
)

// Request types
type RequestMessage struct {
	ID   int64       `json:"id"`
	Type string      `json:"type"`
	SQL  string      `json:"sql,omitempty"`
	File string      `json:"file,omitempty"`
	VFS  string      `json:"vfs,omitempty"`
	Wasm js.Value    `json:"wasm,omitempty"`
	Params []interface{} `json:"params,omitempty"`
}

// Response types
type ResponseMessage struct {
	ID            int64           `json:"id"`
	OK            bool            `json:"ok"`
	Error         string          `json:"error,omitempty"`
	Columns       []string        `json:"columns,omitempty"`
	Rows          [][]interface{} `json:"rows,omitempty"`
	RowsAffected  *int64          `json:"rowsAffected,omitempty"`
	LastInsertID  *int64          `json:"lastInsertId,omitempty"`
}

// convertParamsToJS converts Go driver values to JavaScript values
func convertParamsToJS(params []driver.Value) []interface{} {
	jsParams := make([]interface{}, len(params))
	for i, param := range params {
		jsParams[i] = convertValueToJS(param)
	}
	return jsParams
}

// convertValueToJS converts a single Go driver.Value to a JavaScript-compatible value
func convertValueToJS(value driver.Value) interface{} {
	if value == nil {
		return nil
	}
	
	switch v := value.(type) {
	case string:
		return v
	case int64:
		return float64(v) // JavaScript numbers are float64
	case float64:
		return v
	case bool:
		return v
	case []byte:
		// Convert []byte to Uint8Array for JavaScript
		uint8Array := js.Global().Get("Uint8Array").New(len(v))
		js.CopyBytesToJS(uint8Array, v)
		return uint8Array
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		// Fallback to string conversion
		return fmt.Sprintf("%v", v)
	}
}

// convertJSValueToGo converts a JavaScript value to a Go driver.Value
func convertJSValueToGo(jsValue js.Value) driver.Value {
	switch jsValue.Type() {
	case js.TypeNull, js.TypeUndefined:
		return nil
	case js.TypeBoolean:
		return jsValue.Bool()
	case js.TypeNumber:
		num := jsValue.Float()
		// Try to return as int64 if it's a whole number
		if num == float64(int64(num)) {
			return int64(num)
		}
		return num
	case js.TypeString:
		return jsValue.String()
	case js.TypeObject:
		// Check if it's a Uint8Array
		if jsValue.Get("constructor").Get("name").String() == "Uint8Array" {
			length := jsValue.Get("length").Int()
			bytes := make([]byte, length)
			js.CopyBytesToGo(bytes, jsValue)
			return bytes
		}
		// Fallback to string representation
		return jsValue.String()
	default:
		return jsValue.String()
	}
}

// convertRowsFromJS converts JavaScript rows to Go driver.Value slices
func convertRowsFromJS(jsRows js.Value) [][]driver.Value {
	if jsRows.Type() != js.TypeObject || jsRows.IsNull() {
		return nil
	}
	
	length := jsRows.Length()
	rows := make([][]driver.Value, length)
	
	for i := 0; i < length; i++ {
		jsRow := jsRows.Index(i)
		if jsRow.Type() != js.TypeObject || jsRow.IsNull() {
			continue
		}
		
		rowLength := jsRow.Length()
		row := make([]driver.Value, rowLength)
		
		for j := 0; j < rowLength; j++ {
			row[j] = convertJSValueToGo(jsRow.Index(j))
		}
		
		rows[i] = row
	}
	
	return rows
}

// convertColumnsFromJS converts JavaScript column names to Go string slice
func convertColumnsFromJS(jsColumns js.Value) []string {
	if jsColumns.Type() != js.TypeObject || jsColumns.IsNull() {
		return nil
	}
	
	length := jsColumns.Length()
	columns := make([]string, length)
	
	for i := 0; i < length; i++ {
		columns[i] = jsColumns.Index(i).String()
	}
	
	return columns
}

// parseJSResponse parses a JavaScript response object into a ResponseMessage
func parseJSResponse(jsResponse js.Value) (resp *ResponseMessage, err error) {
	// Use defer to catch any panics from invalid JS values and return error instead
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic in parseJSResponse: %v\n", r)
			err = fmt.Errorf("failed to parse response: %v", r)
			resp = nil
		}
	}()
	
	// First check if the response is valid
	if jsResponse.IsNull() || jsResponse.IsUndefined() {
		return nil, fmt.Errorf("invalid response: null or undefined")
	}
	
	response := &ResponseMessage{}
	
	// Parse ID safely
	idVal := jsResponse.Get("id")
	if !idVal.IsUndefined() && !idVal.IsNull() {
		response.ID = int64(idVal.Float())
	}
	
	// Parse OK flag
	okVal := jsResponse.Get("ok")
	if !okVal.IsUndefined() && !okVal.IsNull() {
		response.OK = okVal.Bool()
	}
	
	if !response.OK {
		if errVal := jsResponse.Get("error"); !errVal.IsUndefined() {
			response.Error = errVal.String()
		}
		return response, nil
	}
	
	// Parse columns
	if columns := jsResponse.Get("columns"); !columns.IsUndefined() {
		response.Columns = convertColumnsFromJS(columns)
	}
	
	// Parse rows
	if rows := jsResponse.Get("rows"); !rows.IsUndefined() {
		jsRows := convertRowsFromJS(rows)
		response.Rows = make([][]interface{}, len(jsRows))
		for i, row := range jsRows {
			response.Rows[i] = make([]interface{}, len(row))
			for j, val := range row {
				response.Rows[i][j] = val
			}
		}
	}
	
	// Parse rowsAffected - be extra careful with type checking
	affectedVal := jsResponse.Get("rowsAffected")
	if !affectedVal.IsUndefined() && !affectedVal.IsNull() {
		// Only process if it's not undefined/null
		val := int64(affectedVal.Float())
		response.RowsAffected = &val
	}
	
	// Parse lastInsertId - be extra careful with type checking
	insertIDVal := jsResponse.Get("lastInsertId")
	if !insertIDVal.IsUndefined() && !insertIDVal.IsNull() {
		// Only process if it's not undefined/null
		val := int64(insertIDVal.Float())
		response.LastInsertID = &val
	}
	
	return response, nil
}

// createJSRequest creates a JavaScript request object from Go parameters
func createJSRequest(id int64, msgType string, params map[string]interface{}) js.Value {
	request := js.Global().Get("Object").New()
	request.Set("id", id)
	request.Set("type", msgType)
	
	for key, value := range params {
		if value != nil {
			switch v := value.(type) {
			case []driver.Value:
				jsArray := js.Global().Get("Array").New(len(v))
				for i, param := range v {
					jsArray.SetIndex(i, convertValueToJS(param))
				}
				request.Set(key, jsArray)
			default:
				request.Set(key, v)
			}
		}
	}
	
	return request
}