import { render } from 'preact';
import { App } from './App.jsx';

const root = document.getElementById('checkouts-app');
if (root) {
  render(<App />, root);
}
