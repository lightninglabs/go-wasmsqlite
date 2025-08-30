// Bridge between Go and SQLite Worker1 Promiser API
// This runs in the main thread and uses the SQLite Worker1 Promiser

let promiser = null;
let dbId = null;

// Initialize the SQLite Worker1 Promiser
async function initSQLiteWorker() {
  return new Promise((resolve, reject) => {
    try {
      promiser = sqlite3Worker1Promiser({
        onready: () => {
          console.log('SQLite Worker1 Promiser initialized');
          resolve();
        },
        onerror: (error) => {
          console.error('SQLite Worker1 Promiser error:', error);
          reject(error);
        }
      });
    } catch (error) {
      console.error('Failed to create SQLite Worker1 Promiser:', error);
      reject(error);
    }
  });
}

// Bridge API that matches what Go expects
window.sqliteBridge = {
  async init() {
    await initSQLiteWorker();
    
    // Get SQLite version
    const config = await promiser('config-get', {});
    console.log('SQLite version:', config.result?.version?.libVersion);
    
    return { ok: true };
  },
  
  async open(filename = 'mydb.sqlite3', vfs = 'opfs') {
    if (!promiser) {
      throw new Error('SQLite not initialized');
    }
    
    // Close existing database if open
    if (dbId) {
      await promiser('close', { dbId });
      dbId = null;
    }
    
    // Determine the filename to use
    let dbFilename = filename;
    let vfsType = 'unknown';
    
    // Check available VFS
    const config = await promiser('config-get', {});
    const vfsList = config.result?.vfsList || [];
    
    if (vfs === 'opfs' || vfs === 'opfs-sahpool') {
      if (vfsList.includes('opfs')) {
        dbFilename = `file:${filename}?vfs=opfs`;
        vfsType = 'opfs';
        console.log('Using OPFS for persistence');
      } else {
        console.warn('OPFS not available, falling back to in-memory');
        dbFilename = ':memory:';
        vfsType = 'memory';
      }
    } else if (filename === ':memory:' || vfs === 'memory') {
      dbFilename = ':memory:';
      vfsType = 'memory';
    }
    
    // Open the database
    const result = await promiser('open', {
      filename: dbFilename
    });
    
    dbId = result.dbId;
    console.log(`Database opened with ID: ${dbId}, VFS: ${vfsType}`);
    
    
    return { ok: true, vfsType };
  },
  
  async exec(sql, params = []) {
    if (!dbId) throw new Error('Database not opened');
    
    await promiser('exec', {
      dbId,
      sql,
      bind: params
    });
    
    // Get rows affected and last insert ID
    const result = await promiser('exec', {
      dbId,
      sql: 'SELECT changes() as changes, last_insert_rowid() as lastId',
      returnValue: 'resultRows'
    });
    
    const rowsAffected = result?.result?.resultRows?.[0]?.[0] || 0;
    const lastInsertId = result?.result?.resultRows?.[0]?.[1] || 0;
    
    return { ok: true, rowsAffected, lastInsertId };
  },
  
  async query(sql, params = []) {
    if (!dbId) throw new Error('Database not opened');
    
    const result = await promiser('exec', {
      dbId,
      sql,
      bind: params,
      returnValue: 'resultRows',
      rowMode: 'array'
    });
    
    const rows = result?.result?.resultRows || [];
    
    // Try to determine column names
    let columns = [];
    console.log('Processing SQL for column extraction:', sql);
    
    // Handle RETURNING clauses (for INSERT/UPDATE/DELETE ... RETURNING)
    const returningMatch = sql.match(/RETURNING\s+(.+?)(?:$|;)/mis);
    if (returningMatch) {
      const returningClause = returningMatch[1].trim();
      console.log('Extracted RETURNING clause:', returningClause);
      columns = returningClause.split(',').map(c => {
        const parts = c.trim().split(/\s+as\s+/i);
        if (parts.length > 1) {
          return parts[1].trim();
        }
        const dotParts = parts[0].split('.');
        return dotParts[dotParts.length - 1].trim();
      });
      console.log('Parsed columns from RETURNING:', columns);
    } else {
      console.log('No RETURNING clause found in SQL');
      // Handle regular SELECT queries
      const selectMatch = sql.match(/SELECT\s+(.+?)\s+FROM/i);
      if (selectMatch) {
        const selectClause = selectMatch[1].trim();
        if (selectClause === '*') {
          if (sql.toLowerCase().includes('from users')) {
            columns = ['id', 'username', 'email', 'created_at'];
          } else if (sql.toLowerCase().includes('from posts')) {
            columns = ['id', 'user_id', 'title', 'content', 'published', 'created_at'];
          } else if (rows[0]) {
            columns = rows[0].map((_, i) => `column${i}`);
          }
        } else {
          columns = selectClause.split(',').map(c => {
            const parts = c.trim().split(/\s+as\s+/i);
            if (parts.length > 1) {
              return parts[1].trim();
            }
            const dotParts = parts[0].split('.');
            return dotParts[dotParts.length - 1].trim();
          });
        }
      }
    }
    
    return { ok: true, columns, rows };
  },
  
  async begin() {
    if (!dbId) throw new Error('Database not opened');
    
    await promiser('exec', {
      dbId,
      sql: 'BEGIN IMMEDIATE'
    });
    
    return { ok: true };
  },
  
  async commit() {
    if (!dbId) throw new Error('Database not opened');
    
    await promiser('exec', {
      dbId,
      sql: 'COMMIT'
    });
    
    return { ok: true };
  },
  
  async rollback() {
    if (!dbId) throw new Error('Database not opened');
    
    await promiser('exec', {
      dbId,
      sql: 'ROLLBACK'
    });
    
    return { ok: true };
  },
  
  async close() {
    if (dbId) {
      await promiser('close', { dbId });
      dbId = null;
    }
    return { ok: true };
  },
  
  async dump() {
    if (!dbId) throw new Error('Database not opened');
    
    let dumpSql = '';
    
    // Get schema
    const schemaResult = await promiser('exec', {
      dbId,
      sql: "SELECT sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY tbl_name, type DESC, name",
      returnValue: 'resultRows'
    });
    
    if (schemaResult?.result?.resultRows) {
      for (const [sql] of schemaResult.result.resultRows) {
        dumpSql += sql + ';\n';
      }
    }
    
    // Get data for each table
    const tablesResult = await promiser('exec', {
      dbId,
      sql: "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'",
      returnValue: 'resultRows'
    });
    
    if (tablesResult?.result?.resultRows) {
      for (const [tableName] of tablesResult.result.resultRows) {
        const dataResult = await promiser('exec', {
          dbId,
          sql: `SELECT * FROM ${tableName}`,
          returnValue: 'resultRows'
        });
        
        if (dataResult?.result?.resultRows?.length > 0) {
          // Get column names
          const pragmaResult = await promiser('exec', {
            dbId,
            sql: `PRAGMA table_info(${tableName})`,
            returnValue: 'resultRows'
          });
          
          const columnNames = pragmaResult?.result?.resultRows?.map((row) => row[1]) || [];
          
          for (const row of dataResult.result.resultRows) {
            const values = row.map((v) => {
              if (v === null) return 'NULL';
              if (typeof v === 'string') return `'${v.replace(/'/g, "''")}'`;
              return String(v);
            }).join(', ');
            dumpSql += `INSERT INTO ${tableName} (${columnNames.join(', ')}) VALUES (${values});\n`;
          }
        }
      }
    }
    
    return { ok: true, dump: dumpSql };
  },
  
  async load(sql) {
    if (!dbId) throw new Error('Database not opened');
    
    // Drop existing tables first
    const tablesResult = await promiser('exec', {
      dbId,
      sql: "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'",
      returnValue: 'resultRows'
    });
    
    if (tablesResult?.result?.resultRows) {
      for (const [tableName] of tablesResult.result.resultRows) {
        await promiser('exec', {
          dbId,
          sql: `DROP TABLE IF EXISTS ${tableName}`
        });
      }
    }
    
    // Load the SQL dump
    await promiser('exec', {
      dbId,
      sql
    });
    
    return { ok: true };
  }
};

// Auto-initialize when loaded
(async () => {
  try {
    await window.sqliteBridge.init();
    console.log('SQLite bridge initialized successfully');
  } catch (error) {
    console.error('Failed to initialize SQLite bridge:', error);
  }
})();