import esbuild from 'esbuild';

const shouldWatch = process.argv.includes('--watch');

const config = {
  entryPoints: ['src/bridge.worker.ts'],
  outfile: '../assets/bridge.worker.js',
  bundle: true,
  format: 'iife',
  platform: 'browser',
  target: ['es2020'],
  minify: true,
  sourcemap: false,
};

if (shouldWatch) {
  const context = await esbuild.context(config);
  await context.watch();
  console.log('Watching for changes...');
} else {
  await esbuild.build(config);
  console.log('Build complete!');
}