import "@testing-library/jest-dom/vitest";
import "./src/i18n";

// jsdom 没有实现 matchMedia，Refine/Radix 生态组件在测试中可能引用它。
if (!window.matchMedia) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
    }),
  });
}
