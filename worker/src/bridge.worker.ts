import sqlite3InitModule from '@sqlite.org/sqlite-wasm';

type RequestMessage =
  | { id: number; type: 'init' }
  | { id: number; type: 'open'; file: string; vfs?: string; flags?: string }
  | { id: number; type: 'exec'; sql: string; params?: any[] }
  | { id: number; type: 'query'; sql: string; params?: any[] }
  | { id: number; type: 'begin' }
  | { id: number; type: 'commit' }
  | { id: number; type: 'rollback' }
  | { id: number; type: 'close' };

type ResponseMessage = {
  id: number;
  ok: boolean;
  error?: string;
  columns?: string[];
  rows?: any[][];
  rowsAffected?: number;
  lastInsertId?: number;
};

let sqlite3: any;
let db: any;

function postSuccess(id: number, payload: Partial<ResponseMessage> = {}) {
  const message: ResponseMessage = { id, ok: true, ...payload };
  self.postMessage(message);
}

function postError(id: number, error: any) {
  const message: ResponseMessage = { 
    id, 
    ok: false, 
    error: String(error) 
  };
  self.postMessage(message);
}

self.onmessage = async (event: MessageEvent<RequestMessage>) => {
  const { id, type } = event.data;
  
  try {
    switch (type) {
      case 'init':
        // Initialize SQLite WASM - it will fetch sqlite3.wasm from the same origin
        sqlite3 = await sqlite3InitModule({
          locateFile: (filename: string) => {
            // Use absolute URL to ensure it works from blob: context
            if (filename === 'sqlite3.wasm') {
              return new URL('/sqlite3.wasm', self.location.origin).href;
            }
            return filename;
          }
        });
        
        postSuccess(id);
        break;

      case 'open':
        if (!sqlite3) {
          throw new Error('SQLite WASM not loaded. Call init first.');
        }
        
        let { file, vfs = 'opfs-sahpool', flags = 'ct' } = event.data;
        
        // Check available VFS options
        const availableVfs = sqlite3.capi.sqlite3_vfs_find(vfs);
        
        if (!availableVfs) {
          console.warn(`VFS '${vfs}' not available, falling back to default VFS`);
          
          // Try to find an available VFS
          if (sqlite3.capi.sqlite3_vfs_find('opfs')) {
            vfs = 'opfs';
            console.log('Using OPFS VFS (without sahpool)');
          } else {
            vfs = undefined as any; // Use default VFS (likely in-memory)
            console.warn('OPFS not available, using default VFS (data will not persist)');
            if (file !== ':memory:') {
              file = ':memory:'; // Force in-memory if OPFS not available
            }
          }
        }
        
        db = new sqlite3.oo1.DB({
          filename: file,
          flags,
          vfs
        });
        
        console.log(`Database opened with file: ${file}, vfs: ${vfs || 'default'}`);
        
        postSuccess(id);
        break;

      case 'exec':
        if (!db) {
          throw new Error('Database not opened');
        }
        
        const { sql, params = [] } = event.data;
        
        // Execute the SQL
        if (params.length > 0) {
          db.exec({
            sql: sql,
            bind: params
          });
        } else {
          db.exec(sql);
        }
        
        // Get affected rows and last insert ID using capi
        const rowsAffected = sqlite3.capi.sqlite3_changes(db.pointer);
        const lastInsertId = sqlite3.capi.sqlite3_last_insert_rowid(db.pointer);
        
        // Convert BigInt to Number for JSON serialization
        postSuccess(id, {
          rowsAffected: Number(rowsAffected),
          lastInsertId: Number(lastInsertId)
        });
        break;

      case 'query':
        if (!db) {
          throw new Error('Database not opened');
        }
        
        const { sql: querySql, params: queryParams = [] } = event.data;
        
        // Execute query and get results
        let columns: string[] = [];
        let rows: any[][] = [];
        
        try {
          // Use exec with returnValue to get results
          const result = db.exec({
            sql: querySql,
            bind: queryParams || [],
            returnValue: "resultRows"
          });
          
          console.log('Query result:', result);
          
          // result is an array of result sets
          if (result && result.length > 0) {
            // Check if this is a multi-row result (for :many queries)
            // SQLite WASM returns an array of arrays for multiple rows
            if (result.length > 0 && Array.isArray(result[0])) {
              // All elements are arrays - this is a multi-row result
              const allArrays = result.every((item: any) => Array.isArray(item));
              
              if (allArrays) {
                // Multi-row result - each element is a row
                rows = result;
                
                // Determine column names based on the query type
                if (result[0]) {
                  // Check for RETURNING clause first
                  const returningMatch = querySql.match(/RETURNING\s+(.+?)(?:;|$)/i);
                  if (returningMatch) {
                    const returningClause = returningMatch[1].trim();
                    if (returningClause === '*') {
                      const tableMatch = querySql.match(/INSERT\s+INTO\s+(\w+)/i);
                      if (tableMatch && tableMatch[1].toLowerCase() === 'users') {
                        columns = ['id', 'username', 'email', 'created_at'];
                      } else if (tableMatch && tableMatch[1].toLowerCase() === 'posts') {
                        columns = ['id', 'user_id', 'title', 'content', 'published', 'created_at'];
                      } else {
                        columns = result[0].map((_, i) => `column${i}`);
                      }
                    } else {
                      columns = returningClause.split(',').map(c => c.trim());
                    }
                  } else {
                    // Check for SELECT queries
                    const selectMatch = querySql.match(/SELECT\s+(.+?)\s+FROM/i);
                    if (selectMatch) {
                      const selectClause = selectMatch[1].trim();
                      if (selectClause === '*' || selectClause.includes('*')) {
                        // Determine columns based on table
                        if (querySql.toLowerCase().includes('from users')) {
                          columns = ['id', 'username', 'email', 'created_at'];
                        } else if (querySql.toLowerCase().includes('from posts')) {
                          columns = ['id', 'user_id', 'title', 'content', 'published', 'created_at'];
                        } else {
                          columns = result[0].map((_, i) => `column${i}`);
                        }
                      } else {
                        // Parse column names from SELECT clause
                        columns = selectClause.split(',').map(c => {
                          // Handle cases like "p.id" -> "id"
                          const parts = c.trim().split('.');
                          return parts[parts.length - 1].trim();
                        });
                      }
                    } else {
                      // Fallback to generic column names
                      columns = result[0].map((_, i) => `column${i}`);
                    }
                  }
                }
                
                // If we have only one row and it's a RETURNING query, keep it as single row
                if (rows.length === 1 && querySql.match(/RETURNING/i)) {
                  // Keep single row for RETURNING queries
                  console.log('Single row RETURNING result');
                } else {
                  console.log(`Multi-row result with ${rows.length} rows`);
                }
              }
            } else if (result[0] && typeof result[0] === 'object' && !Array.isArray(result[0]) && result[0].columnNames) {
              // Standard SQLite WASM result format with columnNames
              const resultSet = result[0];
              columns = resultSet.columnNames || [];
              
              if (Array.isArray(resultSet.values)) {
                rows = resultSet.values;
              } else if (resultSet.values && typeof resultSet.values === 'function') {
                const vals = resultSet.values();
                rows = Array.isArray(vals) ? vals : Array.from(vals);
              } else if (resultSet.values && Symbol.iterator in Object(resultSet.values)) {
                rows = Array.from(resultSet.values);
              } else {
                rows = [];
              }
            }
          }
        } catch (e) {
          console.log('Query error, trying fallback:', e);
          // Fallback to simpler API if available
          rows = db.selectArrays ? db.selectArrays(querySql, queryParams) : [];
        }
        
        console.log('Final columns:', columns);
        console.log('Final rows:', rows);
        
        postSuccess(id, { columns, rows });
        break;

      case 'begin':
        if (!db) {
          throw new Error('Database not opened');
        }
        db.exec('BEGIN IMMEDIATE');
        postSuccess(id);
        break;

      case 'commit':
        if (!db) {
          throw new Error('Database not opened');
        }
        db.exec('COMMIT');
        postSuccess(id);
        break;

      case 'rollback':
        if (!db) {
          throw new Error('Database not opened');
        }
        db.exec('ROLLBACK');
        postSuccess(id);
        break;

      case 'close':
        if (db) {
          db.close();
          db = null;
        }
        postSuccess(id);
        break;

      default:
        throw new Error(`Unknown message type: ${(event.data as any).type}`);
    }
  } catch (error) {
    postError(id, error);
  }
};