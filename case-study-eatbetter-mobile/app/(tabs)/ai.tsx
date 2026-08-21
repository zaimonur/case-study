import { StyleSheet, Text, View } from 'react-native';

export default function AiScreen() {
  return (
    <View style={styles.container}>
      <Text style={styles.title}>AI</Text>
      <Text style={styles.description}>
        AI ile öğün anlama ve danışmanlık bu görev kapsamının dışındadır.
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, justifyContent: 'center', padding: 28, backgroundColor: '#f7faf8' },
  title: { color: '#1d2b26', fontSize: 28, fontWeight: '700' },
  description: { marginTop: 10, color: '#64716c', fontSize: 16, lineHeight: 24 },
});
