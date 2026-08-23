import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

// No `globals: true` in vite.config.ts (test files import from 'vitest'
// explicitly), so @testing-library/react's automatic afterEach cleanup
// never registers itself — without this, DOM from one test's render()
// leaks into the next test in the same file.
afterEach(cleanup)
