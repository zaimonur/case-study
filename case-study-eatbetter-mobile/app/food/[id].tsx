import { useLocalSearchParams } from 'expo-router';
import { StyleSheet, Text, View } from 'react-native';

export default function FoodDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string | string[] }>();
  const foodId = Array.isArray(id) ? id[0] : id;

  return (
    <View style={styles.container}>
      <Text style={styles.title}>Yiyecek Detayı</Text>
      <Text style={styles.description}>Detay görünümü sonraki görevde eklenecek.</Text>
      {foodId ? <Text style={styles.identifier}>Yiyecek kimliği: {foodId}</Text> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, justifyContent: 'center', padding: 28, backgroundColor: '#f7faf8' },
  title: { color: '#1d2b26', fontSize: 28, fontWeight: '700' },
  description: { marginTop: 10, color: '#64716c', fontSize: 16, lineHeight: 24 },
  identifier: { marginTop: 18, color: '#7b8782', fontSize: 14 },
});
