import { createElement } from 'preact';

function shallowDiffers(a, b) {
  for (const i in a) if (i !== '__source' && a[i] !== b[i]) return true;
  for (const i in b) if (i !== '__source' && !(i in a)) return true;
  return false;
}

// Minimal local equivalent of preact/compat's memo (MIT; adapted from
// Preact's compat implementation). Importing preact/compat installs its global
// vnode/event hooks, which added ~3.6 kB and made the 1s clock tick cost
// roughly double. This keeps the same memo API and comparator semantics
// without pulling in the React compatibility runtime.
export function memo(InnerComponent, comparer) {
  function shouldUpdate(nextProps) {
    const ref = this.props.ref;
    if (ref != nextProps.ref && ref) {
      typeof ref === 'function' ? ref(null) : (ref.current = null);
    }
    return comparer
      ? !comparer(this.props, nextProps) || ref != nextProps.ref
      : shallowDiffers(this.props, nextProps);
  }

  function Memoed(props) {
    this.shouldComponentUpdate = shouldUpdate;
    return createElement(InnerComponent, props);
  }
  Memoed.displayName = `Memo(${InnerComponent.displayName || InnerComponent.name})`;
  Memoed.prototype.isReactComponent = true;
  Memoed.type = InnerComponent;
  return Memoed;
}
