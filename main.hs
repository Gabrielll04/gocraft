{-# LANGUAGE DeriveGeneric #-}
{-# LANGUAGE DuplicateRecordFields #-}

module Main (main) where

import Data.Bits
import Data.Word
import GHC.Generics
import Network.Socket
import Data.Serialize
import Data.Aeson as A
import Prelude hiding (max)
import Control.Monad (unless)
import qualified Data.Text as T
import Control.Concurrent (forkIO)
import Data.Text.Encoding (decodeUtf8)
import qualified Data.ByteString as BS
import qualified Data.ByteString.Lazy as B
import Network.Socket.ByteString (recv, sendAll)

-- Types

type VarInt = Integer

data ServerboundPackets 
    = Handshake VarInt T.Text Word16 VarInt
    | Request
    | Ping Word64
    deriving (Show, Eq)

data ClientboundPackets
    = Pong Word64
    | Response PingResponse

data PingResponse = PingResponse 
    {   version     :: Version
    ,   players     :: Players
    ,   description :: Description
    }
    deriving (Show, Generic)

instance ToJSON PingResponse where
  toEncoding = genericToEncoding defaultOptions

data Version = Version 
    {   name     :: String
    ,   protocol :: Int
    }
    deriving (Show, Generic)

instance ToJSON Version where
  toEncoding = genericToEncoding defaultOptions

data Players = Players 
    {   max    :: Int
    ,   online :: Int
    ,   sample :: [Player]
    }
    deriving (Show, Generic)

instance ToJSON Players where
  toEncoding = genericToEncoding defaultOptions

data Description
    = Description { text :: String }
    deriving (Show, Generic)

instance ToJSON Description where
  toEncoding = genericToEncoding defaultOptions

data Player = Player 
    {   name :: String
    ,   id   :: String
    }
    deriving (Show, Generic)

instance ToJSON Player where
  toEncoding = genericToEncoding defaultOptions

data PacketState = Handshaking | Status deriving (Show, Generic)

-- Methods to varints

readVarInt :: Get VarInt
readVarInt = do
    bs <- loop
    return $! toInt bs
    where
        segment_bits = 0x7F
        continue_bit = 0x80
        loop = do
            b <- getWord8
            if (b .&. continue_bit) /= 0
                then do
                    bs <- loop
                    return ((b .&. segment_bits):bs)
                else return [b]
        toInt = foldr (\b i -> (i `shiftL` 7) .|. toInteger b) 0

toWord :: Integer -> Word8
toWord i = fromInteger i :: Word8

writeVarInt :: Putter VarInt
writeVarInt = loop
    where 
        loop value = do
            if value .&. (-128) == 0 -- if 128 (0x80 or CONTINUE_BIT)
                then putWord8 $ toWord value
                else do
                    putWord8 $ toWord value .&. 127 .|. 128
                    let shiftedValue = value `shiftR` 7 -- I think it's desnecessary using a uShiftR function since Word8 is unsigned, but i'm not sure
                    loop shiftedValue

-- Methods to other types
getString :: Get T.Text
getString = do
    i <- readVarInt
    byteString <- getByteString (fromInteger i)
    return $ decodeUtf8 byteString

readPacket :: PacketState -> Get ServerboundPackets
readPacket state = do
    _ <- readVarInt
    packetId <- readVarInt
    case (state, packetId) of
        (Handshaking, 0x00) -> do
            protocolVersion <- readVarInt
            address <- getString
            port <- getWord16be
            nextState <- readVarInt
            return $! Handshake protocolVersion address port nextState
        (Status, 0x00) -> 
            return Request
        (Status, 0x01) -> do
            payload <- getWord64be
            return $! Ping payload -- $! to void lazyness
        _ -> fail $ "Can't read packet with id: " ++ show packetId

-- The server

main :: IO ()
main = do
    let hints = defaultHints { addrFlags = [AI_PASSIVE], addrSocketType = Stream }
    addr:_ <- getAddrInfo (Just hints) (Just "127.0.0.1") (Just "25565")
    sock <- socket (addrFamily addr) (addrSocketType addr) (addrProtocol addr)
    bind sock (addrAddress addr)
    listen sock 1024
    -- socketAddress <- getSocketName sock
    -- putStrLn "Server " ++ show socketAddress ++ " is running!"
    print "The server is running!"
    mainLoop sock

mainLoop :: Socket -> IO ()
mainLoop sock = do
    (conn,_) <- accept sock
    forkIO $ runConn conn
    mainLoop sock

runConn :: Socket -> IO ()
runConn conn = do
    msg <- recv conn 1024
    let receivedPacket = runGet (readPacket Handshaking) msg
    print $ show receivedPacket
    res <- either fail handlePacket receivedPacket
    let byteresponse = maybe BS.empty (runPut . writePacket) res
    print "before unless"
    print $ show byteresponse
    unless (BS.null byteresponse) $ do
        (sendAll conn byteresponse)
        print $ "Sent response " <> show byteresponse ++ "!"

putString :: Putter BS.ByteString
putString s = do
    writeVarInt $ toInteger $ BS.length s -- as with all strings this is prefixed by its length as a VarInt
    putByteString s

writePacketFields :: Word16 -> Put -> Put
writePacketFields packetId packetData = do
    let packet = runPut packetData
    let len = toInteger $ 1 + BS.length packet
    writeVarInt len
    writeVarInt $ toInteger packetId
    putByteString packet

writePacket :: Putter ClientboundPackets
writePacket (Response msg) = writePacketFields 0x00 $ do
    let encoded = A.encode msg
    putString $ BS.toStrict encoded
writePacket (Pong payload) = writePacketFields 0x01 $ do putWord64be payload

handlePacket :: ServerboundPackets -> IO (Maybe ClientboundPackets)
handlePacket serverpacket =
    case serverpacket of 
        (Handshake _ _ _ nextstate) -> do
            case nextstate of
                2 -> error "not supported yet"
                1 -> pure Nothing
                _ -> error "not supported state"
        Request -> do
            return . Just $ Response resp
        (Ping payload) -> do
            return . Just $ Pong payload

resp :: PingResponse
resp =  PingResponse
        { version = Version "1.19" 759,
          players =
            Players
              { max = 100,
                online = 0,
                sample = []
              },
          description = Description "Hello Haskell"
        }