const path = require('path');
const webpack = require('webpack');
const CopyPlugin = require('copy-webpack-plugin');
const TerserPlugin = require('terser-webpack-plugin');
const MiniCssExtractPlugin = require('mini-css-extract-plugin');
const FaviconsWebpackPlugin = require('favicons-webpack-plugin');

const fs = require('fs');

const themes = ['night'];

// Discover supported locales from the filesystem at build time.
// Mirrors services/i18n/i18n.go discoverLocales(): only 2-letter `xx.json`
// files count, EN first, others alphabetical. Injected into the JS bundle
// as a global constant via DefinePlugin so lib/i18n.js doesn't need a
// hardcoded list — drop a `xx.json` and the JS list updates on next build.
function discoverSupportedLocales() {
    const codes = fs.readdirSync('./locales')
        .filter((f) => /^[a-z]{2}\.json$/.test(f))
        .map((f) => f.replace(/\.json$/, ''))
        .sort();
    const en = codes.filter((c) => c === 'en');
    const rest = codes.filter((c) => c !== 'en');
    return [...en, ...rest];
}

function getEntries(path, ext, prefix = '') {
    return new Promise((resolve) => {
        fs.readdir(path, { recursive: true }, (err, files) => {
            const entries = {};
            for (const f of files) {
                if (f.endsWith(ext)) entries[prefix + f.replace(ext, '')] = path + '/' + f;
            }
            resolve(entries);
        });
    })
}


module.exports = async (env, options) => {
    const jsEntries = {
        ...(await getEntries('./assets/src/js/app', '.js')),
        ...(await getEntries('./assets/src/js/app', '.jsx')),
    };
    const styleEntries = await getEntries('./assets/src/styles', '.css');
    const devMode = options.mode !== 'production';
    const devEntries = devMode ? await getEntries('./assets/src/js/dev', '.js', 'dev/') : {};
    const plugins = [
        new webpack.DefinePlugin({
            // Build-time-derived list of supported locales (see top of file).
            // Consumed by assets/src/js/lib/i18n.js as the SUPPORTED constant.
            __SUPPORTED_LOCALES__: JSON.stringify(discoverSupportedLocales()),
        }),
        new MiniCssExtractPlugin({
            filename: '[name].css',
        }),
        new CopyPlugin({
            patterns: [
                { from: 'node_modules/hls.js/dist/hls.min.js', to: 'lib/hls.min.js'},
            ],
        }),
    ];
    for (const t of themes) {
        plugins.push(new FaviconsWebpackPlugin({
            logo: `./assets/src/images/logo-${t}.svg`,
            prefix: `${t}/`,
            favicons: {
                icons: {
                    android: true,
                    appleIcon: false,
                    appleStartup: false,
                    favicons: true,
                    windows: false,
                    yandex: false,
                },
            },
        }));
        plugins.push(new CopyPlugin({
            patterns: [
                { from: `assets/src/images/logo-${t}.svg`, to: `${t}/favicon.svg` },
            ],
        }));
    }
    return {
        entry: {
            ...jsEntries,
            ...styleEntries,
            ...devEntries,
        },
        devtool: 'source-map',
        output: {
            filename: '[name].js',
            chunkFilename: '[name].[chunkhash].js',
            path: path.resolve(__dirname, 'assets', 'dist'),
            clean: true,
        },
        devServer: {
            port: 8083,
            client: {
                webSocketURL: 'auto://0.0.0.0:0/ws',
            },
            static: './assets/dist',
            allowedHosts: 'all',
            devMiddleware: {
                publicPath: '/assets',
                index: false,
            },
            proxy: [
                {
                    context: () => true,
                    target: 'http://127.0.0.1:8080',
                    changeOrigin: true,
                    secure: false,
                },
            ],
            watchFiles: ['templates/**/*.html', 'assets/src/**/*'],
        },
        optimization: {
            // splitChunks disabled: entry points are loaded independently via Go
            // template helpers, which don't support automatic chunk dependencies.
            minimize: true,
            minimizer: [
                new TerserPlugin({ parallel: true }),
            ],
        },
        module: {
            rules: [
                {
                    test: /\.jsx?$/,
                    include: path.resolve(__dirname, 'assets', 'src'),
                    loader: 'babel-loader'
                },
                {
                    test: /\.css$/i,
                    include: path.resolve(__dirname, 'assets', 'src'),
                    use: [
                        devMode ? 'style-loader' : MiniCssExtractPlugin.loader,
                        'css-loader',
                        'postcss-loader'
                    ],
                },
                {
                    test: /\.json$/,
                    include: path.resolve(__dirname, 'locales'),
                    resourceQuery: /prefix=/,
                    type: 'javascript/auto',
                    use: [path.resolve(__dirname, 'assets/webpack/locale-filter-loader.js')],
                },
            ]
        },
        resolve: {
            extensions: ['.js', '.jsx', '.json'],
        },
        plugins,
    };
}