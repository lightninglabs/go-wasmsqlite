const http = require('http');
const fs = require('fs');
const path = require('path');

const PORT = 8081;
const HOST = 'localhost';

const mimeTypes = {
  '.html': 'text/html',
  '.js': 'application/javascript',
  '.wasm': 'application/wasm',
  '.css': 'text/css',
  '.json': 'application/json',
};

const server = http.createServer((req, res) => {
  // Set CORS and security headers for SharedArrayBuffer support
  res.setHeader('Cross-Origin-Opener-Policy', 'same-origin');
  res.setHeader('Cross-Origin-Embedder-Policy', 'require-corp');
  res.setHeader('Cross-Origin-Resource-Policy', 'same-origin');
  
  let filePath = '.' + req.url;
  if (filePath === './') {
    filePath = './index.html';
  }

  const extname = path.extname(filePath);
  const contentType = mimeTypes[extname] || 'application/octet-stream';

  fs.readFile(filePath, (error, content) => {
    if (error) {
      if (error.code === 'ENOENT') {
        console.log(`404: ${req.url}`);
        res.writeHead(404, { 'Content-Type': 'text/plain' });
        res.end('404 Not Found', 'utf-8');
      } else {
        res.writeHead(500);
        res.end(`Server Error: ${error.code}`, 'utf-8');
      }
    } else {
      res.writeHead(200, { 'Content-Type': contentType });
      res.end(content, 'utf-8');
      console.log(`200: ${req.url}`);
    }
  });
});

server.listen(PORT, HOST, () => {
  console.log(`🌐 Server running at http://${HOST}:${PORT}/`);
  console.log('✅ CORS headers enabled for SharedArrayBuffer support');
  console.log('✅ OPFS support enabled on localhost');
  console.log('📝 Press Ctrl+C to stop\n');
});