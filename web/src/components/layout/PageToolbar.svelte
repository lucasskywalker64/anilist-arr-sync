<div class="toolbar">
  <div class="section left">
    <slot name="left" />
    <slot />
  </div>
  <div class="section right">
    <slot name="right" />
  </div>
</div>

<style>
  .toolbar {
    display: flex;
    justify-content: space-between;
    flex: 0 0 auto;
    padding: 0 20px;
    height: var(--toolbarHeight);
    background-color: var(--toolbarBackgroundColor);
    color: var(--toolbarColor);
    border-bottom: 1px solid rgba(0, 0, 0, 0.2);
  }

  .section {
    display: flex;
    align-items: stretch;
  }

  .left {
    flex-grow: 1;
    justify-content: flex-start;
  }

  .right {
    justify-content: flex-end;
  }

  :global(.toolbarSeparator) {
    margin: 10px 20px;
    height: 40px;
    border-right: 1px solid #e5e5e5;
    opacity: 0.35;
  }
</style>
