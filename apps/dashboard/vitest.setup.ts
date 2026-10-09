import "@testing-library/jest-dom/vitest";
import type { TestingLibraryMatchers } from "@testing-library/jest-dom/matchers";

// Vitest 5's expect() returns Assertion<R, T>, which extends Matchers<R, T>.
// @testing-library/jest-dom's vitest types still augment the pre-v4
// Assertion<T> shape, so they never attach to this interface.
declare module "vitest" {
  interface Matchers<
    R extends void | Promise<void> = void | Promise<void>,
    T = unknown,
  > extends TestingLibraryMatchers<any, R> {}
}
