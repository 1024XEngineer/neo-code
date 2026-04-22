import DefaultTheme from 'vitepress/theme'
import './custom.css'
import ArchitectureGrid from './components/ArchitectureGrid.vue'
import CodePanel from './components/CodePanel.vue'
import QuickStartCards from './components/QuickStartCards.vue'

export default {
  ...DefaultTheme,
  enhanceApp({ app }) {
    app.component('ArchitectureGrid', ArchitectureGrid)
    app.component('CodePanel', CodePanel)
    app.component('QuickStartCards', QuickStartCards)
  }
}
