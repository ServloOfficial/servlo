// An inert stand-in for the Monaco loader, for tests that render something with
// an editor somewhere inside it and assert nothing about the editor.
//
// Monaco is five megabytes behind a dynamic import and cannot run in jsdom. A
// test that lets the real one load races its own teardown to finish that import,
// and the run that loses fails with an error naming no test. The two tests that
// assert on editor wiring build their own fakes; everything else wants this.
export const loadMonaco = async () => ({
  editor: {
    create: () => ({
      getValue: () => '',
      setValue: () => {},
      onDidChangeModelContent: () => ({ dispose() {} }),
      updateOptions: () => {},
      addCommand: () => {},
      dispose: () => {},
      getModel: () => null,
      layout: () => {}
    }),
    setTheme: () => {},
    defineTheme: () => {}
  },
  KeyMod: { CtrlCmd: 1 },
  KeyCode: { KeyS: 1 }
});

export const servloThemeName = () => 'servlo-light';
