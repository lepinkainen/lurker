import { handleWSMessageForTests, initForTests, resetForTests, start as startApp } from "./app-core";

export const __handleWSMessage = handleWSMessageForTests;
export const __initForTests = initForTests;
export const __resetForTests = resetForTests;
export const start = startApp;
