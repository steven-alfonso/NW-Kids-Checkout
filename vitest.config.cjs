module.exports = {
    test: {
        environment: 'jsdom',
        exclude: [
            '**/node_modules/**',
            '**/.git/**',
            '**/.worktrees/**',
            '**/worktrees/**',
            '.worktrees/**',
            'test-support/conformance/**',
            'test-results/**'
        ]
    }
};
