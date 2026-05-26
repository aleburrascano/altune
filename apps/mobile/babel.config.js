module.exports = function (api) {
  api.cache(true);
  return {
    presets: ['babel-preset-expo'],
    plugins: [
      // Path aliases must match tsconfig.json paths
      [
        'module-resolver',
        {
          alias: {
            '@': './src',
            '@features': './src/features',
            '@shared': './src/shared',
          },
        },
      ],
    ],
  };
};
