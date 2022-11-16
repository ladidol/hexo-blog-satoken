package org.cuit.epoch.mapper;


import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Mapper;
import org.cuit.epoch.entity.UserInfo;
import org.springframework.stereotype.Repository;

/**
 * @author: ladidol
 * @date: 2022/11/16 17:11
 * @description:
 */
@Mapper
public interface UserInfoMapper extends BaseMapper<UserInfo> {

}